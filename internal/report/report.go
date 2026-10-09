// Package report is the contract between the export pipeline and the
// interfaces that display it: the full-screen dashboard and the plain line
// renderer. It has no dependencies so both sides can import it.
package report

import "time"

// Reporter receives the progress of a run.
type Reporter interface {
	// Phase announces a new stage: authentication, session, export.
	Phase(name string)
	// Plan lists the datasets about to be exported, so they can be shown as
	// pending before they start.
	Plan(labels []string)
	// Task starts a unit of work.
	Task(label string) Task
	// Accounts lists the accounts found on the login.
	Accounts(accounts []Account)
	// Warn reports a recoverable problem.
	Warn(msg string)
	// Ask requests input from the user and blocks until it is given.
	Ask(prompt Prompt) (string, error)
	// Finish reports the outcome of the export.
	Finish(summary Summary)
}

// Task is a unit of work. Exactly one of Done, Skip or Fail settles it.
type Task interface {
	Update(detail string)
	// Progress reports a known amount of work; total is zero when unknown.
	Progress(done, total int)
	Done(detail string)
	Skip(detail string)
	Fail(err error)
}

// Prompt describes a question asked during a run.
type Prompt struct {
	Title       string
	Description string
	Placeholder string
	Secret      bool
	// Deadline, when set, is shown as a countdown.
	Deadline time.Time
	// Alternative is an answer the user can pick with a shortcut instead of
	// typing, such as "sms" to receive the two-factor code by text message.
	Alternative      string
	AlternativeLabel string
}

// Account is one account pair as shown to the user.
type Account struct {
	Label      string
	Securities string
	Cash       string
	Currency   string
}

// File is a dataset written to disk.
type File struct {
	Name string
	Path string
	Rows int
}

// Summary is the outcome of an export.
type Summary struct {
	Files     []File
	Failed    int
	Elapsed   time.Duration
	OutputDir string
}

// Rows totals the rows across files.
func (s Summary) Rows() int {
	total := 0
	for _, f := range s.Files {
		total += f.Rows
	}
	return total
}
