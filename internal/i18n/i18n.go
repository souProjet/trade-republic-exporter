// Package i18n translates the interface. Messages are written in English in
// the code and looked up in a catalog for other languages, falling back to
// English when a translation is missing.
package i18n

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync/atomic"
)

// Language is an interface language.
type Language string

const (
	Auto    Language = "auto"
	English Language = "en"
	French  Language = "fr"
)

// Languages lists the selectable values of the language setting.
var Languages = []Language{Auto, English, French}

var catalogs = map[Language]map[string]string{
	French: french,
}

var current atomic.Value

func init() { current.Store(English) }

// Set selects the language; Auto resolves it from the environment.
func Set(l Language) {
	if l == Auto || l == "" {
		l = Detect()
	}
	if _, ok := catalogs[l]; !ok && l != English {
		l = English
	}
	current.Store(l)
}

// Current is the language in use.
func Current() Language { return current.Load().(Language) }

// T translates msg and formats it with args, like fmt.Sprintf.
func T(msg string, args ...any) string {
	if catalog, ok := catalogs[Current()]; ok {
		if translated, ok := catalog[msg]; ok && translated != "" {
			msg = translated
		}
	}
	if len(args) == 0 {
		return msg
	}
	return fmt.Sprintf(msg, args...)
}

// N marks a message for translation without translating it, for text stored
// in data and translated with T when shown. The catalog test collects both.
func N(msg string) string { return msg }

// Detect guesses the user's language. Explicit LC_ALL and LC_MESSAGES come
// first; on macOS the system language comes next, because terminals often
// export an English LANG regardless of it; LANG comes last.
func Detect() Language {
	for _, env := range []string{"LC_ALL", "LC_MESSAGES"} {
		if l, ok := parse(os.Getenv(env)); ok {
			return l
		}
	}
	if runtime.GOOS == "darwin" {
		if out, err := exec.Command("defaults", "read", "-g", "AppleLanguages").Output(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				if l, ok := parse(strings.Trim(line, " \t\",()")); ok {
					return l
				}
			}
		}
	}
	if l, ok := parse(os.Getenv("LANG")); ok {
		return l
	}
	return English
}

// parse reads a locale such as fr_FR.UTF-8 or fr-FR.
func parse(locale string) (Language, bool) {
	if locale == "" || locale == "C" || locale == "POSIX" {
		return "", false
	}
	code := strings.ToLower(locale)
	if i := strings.IndexAny(code, "_-.@"); i > 0 {
		code = code[:i]
	}
	switch Language(code) {
	case French:
		return French, true
	case English:
		return English, true
	}
	return "", false
}
