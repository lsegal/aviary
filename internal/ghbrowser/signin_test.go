package ghbrowser

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/browser"
)

var _ Browser = (*browser.Manager)(nil)

// fakeBrowser is a Browser whose every tab shows the same page.
type fakeBrowser struct {
	result  string
	evalErr error
	openErr error
	opened  []string
	closed  []string
}

func (b *fakeBrowser) Open(_ context.Context, url string) (string, error) {
	if b.openErr != nil {
		return "", b.openErr
	}
	b.opened = append(b.opened, url)
	return "tab1", nil
}

func (b *fakeBrowser) EvalJS(_ context.Context, _ string, _ string) (string, error) {
	return b.result, b.evalErr
}

func (b *fakeBrowser) CloseTab(tabID string) error {
	b.closed = append(b.closed, tabID)
	return nil
}

func page(result string, err error) Page {
	return tabPage{browser: &fakeBrowser{result: result, evalErr: err}, tabID: "tab1"}
}

const (
	signedOut = `{"signedIn":false,"signedOut":true,"login":""}`
	signedIn  = `{"signedIn":true,"signedOut":false,"login":"octocat"}`
	neither   = `{"signedIn":false,"signedOut":false,"login":""}`
)

func TestSignedOutPage(t *testing.T) {
	ctx := context.Background()
	assert.True(t, SignedOutPage(ctx, page(signedOut, nil)))
	assert.False(t, SignedOutPage(ctx, page(signedIn, nil)))
	assert.False(t, SignedOutPage(ctx, page(neither, nil)), "no evidence is not signed out")
	assert.False(t, SignedOutPage(ctx, page("", errors.New("boom"))), "a failed probe is not signed out")
	assert.False(t, SignedOutPage(ctx, page("not json", nil)))
}

func TestSignedOutStatus(t *testing.T) {
	ctx := context.Background()
	assert.True(t, SignedOutStatus(ctx, page(signedOut, nil), 404))
	assert.True(t, SignedOutStatus(ctx, page(signedOut, nil), 403))
	assert.False(t, SignedOutStatus(ctx, page(signedIn, nil), 404), "a signed-in 404 is a missing target")
	assert.False(t, SignedOutStatus(ctx, page(signedOut, nil), 200), "only 404 and 403 count")
}

func TestCheckEmptyResult(t *testing.T) {
	ctx := context.Background()
	url := "https://github.com/issues?q=assignee%3A%40me"
	err := CheckEmptyResult(ctx, page(signedOut, nil), url, 0)
	require.ErrorIs(t, err, ErrSignedOut)
	assert.Contains(t, err.Error(), url)
	assert.NoError(t, CheckEmptyResult(ctx, page(signedIn, nil), url, 0), "signed in, really no issues")
	assert.NoError(t, CheckEmptyResult(ctx, page(signedOut, nil), url, 3), "rows were read")
}

func TestSignedOutErrorMessage(t *testing.T) {
	for _, err := range []error{
		&SignedOutError{URL: "https://github.com/o/r/issues"},
		&SignedOutError{URL: "https://github.com/o/r/issues", Status: 404},
	} {
		msg := err.Error()
		assert.Contains(t, msg, "https://github.com/o/r/issues")
		assert.Contains(t, msg, "Sign in to GitHub in Aviary's browser")
		assert.Contains(t, msg, "gh polling")
		assert.NotContains(t, strings.ToLower(msg), "glorp")
	}
	assert.Contains(t, (&SignedOutError{URL: "u", Status: 404}).Error(), "HTTP 404")
}

func TestCheckSession(t *testing.T) {
	b := &fakeBrowser{result: signedIn}
	state, err := CheckSession(context.Background(), b)
	require.NoError(t, err)
	assert.True(t, state.SignedIn)
	assert.Equal(t, "octocat", state.Login)
	assert.Equal(t, []string{homeURL}, b.opened)
	assert.Equal(t, []string{"tab1"}, b.closed, "the check closes its tab")

	b = &fakeBrowser{result: signedOut}
	state, err = CheckSession(context.Background(), b)
	require.NoError(t, err)
	assert.True(t, state.SignedOut)

	_, err = CheckSession(context.Background(), &fakeBrowser{openErr: errors.New("no chrome")})
	assert.ErrorContains(t, err, "no chrome")
}

func TestOpenSignIn(t *testing.T) {
	b := &fakeBrowser{}
	tab, err := OpenSignIn(context.Background(), b)
	require.NoError(t, err)
	assert.Equal(t, "tab1", tab)
	assert.Equal(t, []string{SignInURL}, b.opened)
	assert.Empty(t, b.closed, "the sign-in tab stays open")
}
