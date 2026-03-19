// Package cronutil centralizes Aviary's accepted cron parsing rules.
package cronutil

import "github.com/robfig/cron/v3"

// Parser accepts standard 5-field cron expressions and 6-field expressions with
// a leading seconds field.
func Parser() cron.Parser {
	return cron.NewParser(
		cron.SecondOptional |
			cron.Minute |
			cron.Hour |
			cron.Dom |
			cron.Month |
			cron.Dow |
			cron.Descriptor,
	)
}

// New returns a cron scheduler configured with Aviary's accepted cron syntax.
func New(opts ...cron.Option) *cron.Cron {
	opts = append([]cron.Option{cron.WithParser(Parser())}, opts...)
	return cron.New(opts...)
}
