package aviary

import (
	"path"
	"testing"

	"gopkg.in/yaml.v3"
)

// Each shipped skill carries the landing.yaml the docs site builds its
// /skills/<name>/ page and share card from (#763).
func TestCodingSkillsHaveLandingPages(t *testing.T) {
	for _, name := range CodingSkillNames {
		data, err := CodingSkills.ReadFile(path.Join(CodingSkillsRoot, name, "landing.yaml"))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var landing struct {
			Eyebrow      string   `yaml:"eyebrow"`
			Headline     string   `yaml:"headline"`
			Tagline      string   `yaml:"tagline"`
			Usage        string   `yaml:"usage"`
			Aviary       string   `yaml:"aviary"`
			Requirements []string `yaml:"requirements"`
			Steps        []struct {
				Title string `yaml:"title"`
				Text  string `yaml:"text"`
			} `yaml:"steps"`
		}
		if err := yaml.Unmarshal(data, &landing); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for field, value := range map[string]string{
			"eyebrow":  landing.Eyebrow,
			"headline": landing.Headline,
			"tagline":  landing.Tagline,
			"usage":    landing.Usage,
			"aviary":   landing.Aviary,
		} {
			if value == "" {
				t.Errorf("%s: landing.yaml has no %s", name, field)
			}
		}
		if len(landing.Steps) == 0 || len(landing.Requirements) == 0 {
			t.Errorf("%s: landing.yaml needs steps and requirements", name)
		}
	}
}
