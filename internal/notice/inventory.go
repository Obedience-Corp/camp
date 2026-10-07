package notice

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// DismissedNotice is one dismissal on record.
//
// The dismissal file keeps an id and a time, never the message, so Notice is
// set only while a detector still reports that id. Subject and Summary come
// from the id itself, so a dismissal nothing reports still reads as more than
// an id.
type DismissedNotice struct {
	ID      string
	At      time.Time
	Subject string
	Summary string
	Notice  *Notice
}

// Inventory is every notice a camp has: the live ones and the dismissed ones.
type Inventory struct {
	Live      []Notice
	Dismissed []DismissedNotice
}

// Empty reports whether there is nothing live and nothing dismissed.
func (inv Inventory) Empty() bool {
	return len(inv.Live) == 0 && len(inv.Dismissed) == 0
}

// TakeInventory runs detectors and partitions what they report against the
// dismissal file.
//
// Unlike FilterDismissed, an unreadable dismissal file is an error here. This
// is the surface where the user manages dismissals, and guessing would hide
// the state they came to look at.
func TakeInventory(ctx context.Context, campaignRoot string, detectors ...Detector) (Inventory, error) {
	dismissals, err := LoadDismissals(campaignRoot)
	if err != nil {
		return Inventory{}, err
	}
	detected := Detect(ctx, campaignRoot, detectors...)
	if err := ctx.Err(); err != nil {
		return Inventory{}, camperrors.Wrap(err, "detect notices")
	}
	return Partition(detected, dismissals, Subjects(campaignRoot)), nil
}

// Partition splits detected notices into live and dismissed. subjects comes
// from Subjects. Separated from the disk load so the decision is testable
// without a filesystem.
//
// Dismissed is ordered newest first, then by id, because the file is a map
// and a listing that reshuffles between runs reads as state changing.
func Partition(detected []Notice, dismissals *DismissalFile, subjects map[string]string) Inventory {
	inv := Inventory{Live: []Notice{}, Dismissed: []DismissedNotice{}}
	stillDetected := map[string]Notice{}
	for _, n := range detected {
		n.Command = FixCommand(n.Command)
		if dismissals.IsDismissed(n.ID) {
			stillDetected[CanonicalID(n.ID)] = n
			continue
		}
		inv.Live = append(inv.Live, n)
	}
	if dismissals == nil {
		return inv
	}

	for id, at := range dismissals.Dismissed {
		d := DismissedNotice{ID: id, At: at, Subject: subjects[id], Summary: Summary(id)}
		if n, ok := stillDetected[id]; ok {
			d.Notice = &n
			if n.Subject != "" {
				d.Subject = n.Subject
			}
		}
		inv.Dismissed = append(inv.Dismissed, d)
	}
	sort.Slice(inv.Dismissed, func(i, j int) bool {
		a, b := inv.Dismissed[i], inv.Dismissed[j]
		if !a.At.Equal(b.At) {
			return a.At.After(b.At)
		}
		return a.ID < b.ID
	})
	return inv
}

// FixCommand returns the command that fixes a notice.
//
// Some detectors append their own dismiss hint to Command so the status line
// carries it. A surface that dismisses in place, or copies the fix to run it,
// wants the fix alone.
func FixCommand(command string) string {
	if i := strings.Index(command, "(dismiss:"); i >= 0 {
		command = command[:i]
	}
	return strings.TrimSpace(command)
}

// ElsewherePrefix opens a fix that has to run on a machine other than this
// one. It is guidance for the reader, not part of the command.
const ElsewherePrefix = "on another machine: "

// RunnableCommand returns the part of a fix a shell can run, and whether the
// fix says to run it on another machine.
func RunnableCommand(command string) (string, bool) {
	command = FixCommand(command)
	if rest, ok := strings.CutPrefix(command, ElsewherePrefix); ok {
		return strings.TrimSpace(rest), true
	}
	return command, false
}

var placeholderPattern = regexp.MustCompile(`<[^<>\s](?:[^<>]*[^<>\s])?>`)

// HasPlaceholder reports whether command names a value the user must fill in,
// such as <machine> or <id of mac-studio>, before it can run.
func HasPlaceholder(command string) bool {
	return placeholderPattern.MatchString(command)
}
