package cliagent

import (
	"context"
	"encoding/base64"
	"errors"
	"mime"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodexExitError(t *testing.T) {
	waitErr := errors.New("exit status 3")
	for _, tc := range []struct {
		name    string
		stderr  string
		waitErr error
		want    string
	}{
		{"stderr wins over the wait error", "  out of credits\n", waitErr, "codex exited: out of credits"},
		{"wait error without stderr", " \n\t", waitErr, "codex exited: exit status 3"},
		{"neither", "", nil, "codex exited before finishing the turn"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := codexExitError(tc.stderr, tc.waitErr)
			assert.EqualError(t, err, tc.want)
			if tc.waitErr != nil && strings.TrimSpace(tc.stderr) == "" {
				assert.ErrorIs(t, err, tc.waitErr, "the wait error stays inspectable")
			}
		})
	}
}

func TestCodexSession_Close(t *testing.T) {
	s, err := NewSession(fakeOptions(t, Codex))
	require.NoError(t, err)
	require.NoError(t, s.Close())
	// Each turn is its own process, so a closed session still runs turns.
	r, err := s.Send(context.Background(), Turn{Text: "after close"}, nil)
	require.NoError(t, err)
	assert.Equal(t, "thread=thread-new images=0 prompt=after close", r.Text)
}

func TestCodexSession_ExitsBeforeFinishing(t *testing.T) {
	for _, tc := range []struct {
		prompt string
		want   string
	}{
		{"crash", "codex exited: codex: out of credits"},
		{"silent-crash", "codex exited: exit status 3"},
		{"quit", "codex exited before finishing the turn"},
	} {
		t.Run(tc.prompt, func(t *testing.T) {
			s, err := NewSession(fakeOptions(t, Codex))
			require.NoError(t, err)
			_, err = s.Send(context.Background(), Turn{Text: tc.prompt}, nil)
			assert.EqualError(t, err, tc.want)

			// The thread it started is still resumed on the next turn.
			r, err := s.Send(context.Background(), Turn{Text: "again"}, nil)
			require.NoError(t, err)
			assert.Equal(t, "thread=thread-new images=0 prompt=again", r.Text)
		})
	}
}

func TestCodexSession_BlankTextSendsImagePlaceholder(t *testing.T) {
	s, err := NewSession(fakeOptions(t, Codex))
	require.NoError(t, err)
	r, err := s.Send(context.Background(), Turn{Text: "  ", MediaURLs: []string{redPixel}}, nil)
	require.NoError(t, err)
	assert.Equal(t, "thread=thread-new images=1 prompt=(see attached image)", r.Text)
}

func TestCodexSession_CanceledMidTurn(t *testing.T) {
	s, err := NewSession(fakeOptions(t, Codex))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err = s.Send(ctx, Turn{Text: "hang"}, func(e Event) {
		if e.Type == EventSession {
			cancel() // codex is now running the turn
		}
	})
	assert.ErrorIs(t, err, context.Canceled)
}

func TestCodexSession_CanceledBeforeStart(t *testing.T) {
	s, err := NewSession(fakeOptions(t, Codex))
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.Send(ctx, Turn{Text: "hello"}, nil)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestCodexSession_BadImageStopsTurn(t *testing.T) {
	opts := fakeOptions(t, Codex)
	opts.Binary = filepath.Join(t.TempDir(), "never-run")
	s, err := NewSession(opts)
	require.NoError(t, err)
	_, err = s.Send(context.Background(), Turn{Text: "hi", MediaURLs: []string{redPixel, "data:image/png,notbase64"}}, nil)
	assert.EqualError(t, err, "image must be a base64 data URL", "the image error is reported before codex starts")
}

func TestCodexSession_ProfileErrorStopsTurn(t *testing.T) {
	opts := fakeOptions(t, Codex)
	opts.Binary = filepath.Join(t.TempDir(), "never-run")
	opts.Env = append(opts.Env, "CODEX_HOME="+filepath.Join(t.TempDir(), "missing"))
	opts.SystemPrompt = "rules"
	s, err := NewSession(opts)
	require.NoError(t, err)
	_, err = s.Send(context.Background(), Turn{Text: "hi"}, nil)
	assert.ErrorContains(t, err, "writing the codex profile")
}

func TestWriteCodexProfile(t *testing.T) {
	home := t.TempDir()

	profile, cleanup, err := writeCodexProfile(Options{SystemPrompt: "rules"})
	require.NoError(t, err)
	assert.Empty(t, profile, "no aviary CODEX_HOME, no profile")
	cleanup()

	profile, cleanup, err = writeCodexProfile(Options{Env: []string{"CODEX_HOME=" + home}})
	require.NoError(t, err)
	assert.Empty(t, profile, "no system prompt, no profile")
	cleanup()

	// The last CODEX_HOME wins, as it does in the child's environment.
	profile, cleanup, err = writeCodexProfile(Options{
		Env:          []string{"CODEX_HOME=" + filepath.Join(home, "missing"), "OTHER=1", "CODEX_HOME=" + home},
		SystemPrompt: "say \"hi\"",
	})
	require.NoError(t, err)
	require.NotEmpty(t, profile)
	data, err := os.ReadFile(filepath.Join(home, profile+codexProfileSuffix))
	require.NoError(t, err)
	assert.Equal(t, "developer_instructions = \"say \\\"hi\\\"\"\n", string(data))
	cleanup()
	entries, err := os.ReadDir(home)
	require.NoError(t, err)
	assert.Empty(t, entries, "cleanup removes the profile")

	profile, cleanup, err = writeCodexProfile(Options{
		Env:          []string{"CODEX_HOME=" + filepath.Join(home, "missing")},
		SystemPrompt: "rules",
	})
	assert.ErrorContains(t, err, "writing the codex profile")
	assert.Empty(t, profile)
	assert.Nil(t, cleanup)
}

func imageServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pixel.gif":
			w.Header().Set("Content-Type", "image/gif")
			_, _ = w.Write([]byte("GIF89a"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchImage(t *testing.T) {
	srv := imageServer(t)
	ctx := context.Background()

	data, mediaType, err := fetchImage(ctx, srv.URL+"/pixel.gif")
	require.NoError(t, err)
	assert.Equal(t, []byte("GIF89a"), data)
	assert.Equal(t, "image/gif", mediaType)

	_, _, err = fetchImage(ctx, srv.URL+"/missing.png")
	assert.EqualError(t, err, "fetching image: HTTP 404")

	closed := httptest.NewServer(http.NotFoundHandler())
	unreachable := closed.URL + "/pixel.gif"
	closed.Close()
	_, _, err = fetchImage(ctx, unreachable)
	assert.ErrorContains(t, err, "fetching image: ")

	_, _, err = fetchImage(ctx, "http://[::1")
	assert.Error(t, err, "a malformed URL is rejected before any request")

	canceled, cancel := context.WithCancel(ctx)
	cancel()
	_, _, err = fetchImage(canceled, srv.URL+"/pixel.gif")
	assert.ErrorIs(t, err, context.Canceled)
}

func TestReadImage(t *testing.T) {
	ctx := context.Background()

	data, mediaType, err := readImage(ctx, "data:image/jpeg;base64,"+base64.StdEncoding.EncodeToString([]byte("jpeg bytes")))
	require.NoError(t, err)
	assert.Equal(t, []byte("jpeg bytes"), data)
	assert.Equal(t, "image/jpeg", mediaType)

	_, _, err = readImage(ctx, "data:image/png,raw")
	assert.EqualError(t, err, "image must be a base64 data URL")

	_, _, err = readImage(ctx, "data:image/png;base64,***")
	assert.ErrorContains(t, err, "decoding image: ")

	srv := imageServer(t)
	data, mediaType, err = readImage(ctx, srv.URL+"/pixel.gif")
	require.NoError(t, err, "anything but a data URL is fetched")
	assert.Equal(t, []byte("GIF89a"), data)
	assert.Equal(t, "image/gif", mediaType)

	_, _, err = readImage(ctx, filepath.Join(t.TempDir(), "local.png"))
	assert.Error(t, err, "a local path is not read from disk")
}

func TestWriteTempImage(t *testing.T) {
	tmp := t.TempDir()
	setTempDir(t, tmp)
	ctx := context.Background()
	srv := imageServer(t)

	firstExt := func(mediaType string) string {
		exts, _ := mime.ExtensionsByType(mediaType)
		require.NotEmpty(t, exts)
		return exts[0]
	}
	for _, tc := range []struct {
		name    string
		url     string
		data    string
		wantExt string
	}{
		{"data URL", "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("jpeg")), "jpeg", firstExt("image/jpeg")},
		{"media type parameters", "data:image/gif; charset=x;base64," + base64.StdEncoding.EncodeToString([]byte("gif")), "gif", firstExt("image/gif")},
		{"unknown media type", "data:image/x-aviary-unknown;base64," + base64.StdEncoding.EncodeToString([]byte("raw")), "raw", ".png"},
		{"remote URL", srv.URL + "/pixel.gif", "GIF89a", firstExt("image/gif")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path, err := writeTempImage(ctx, tc.url)
			require.NoError(t, err)
			defer os.Remove(path) //nolint:errcheck
			assert.Equal(t, tmp, filepath.Dir(path))
			assert.True(t, strings.HasPrefix(filepath.Base(path), "aviary-image-"))
			assert.Equal(t, tc.wantExt, filepath.Ext(path))
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tc.data, string(data))
		})
	}

	_, err := writeTempImage(ctx, srv.URL+"/missing.png")
	assert.EqualError(t, err, "fetching image: HTTP 404")
	_, err = writeTempImage(ctx, "data:image/png;base64,***")
	assert.ErrorContains(t, err, "decoding image: ")
	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	assert.Empty(t, entries, "a failed image leaves no temp file")

	setTempDir(t, filepath.Join(tmp, "missing"))
	_, err = writeTempImage(ctx, redPixel)
	assert.Error(t, err, "a temp directory that cannot be written fails the image")
}

// setTempDir points os.TempDir at dir on every platform.
func setTempDir(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, dir)
	}
}
