package selector

import (
	"cmp"
	"slices"
	"strings"
	"time"
)

type Pin int

const (
	PinNone   Pin = iota
	PinTop        // (none), + New Project
	PinBottom     // reserved; unused in v1
)

// Item is one selectable row. ID is stable identity across SetItems rebuilds.
type Item struct {
	ID     string
	Label  string
	Filter string
	Rank   time.Time
	Pin    Pin
}

type keyedItem struct {
	item Item
	idx  int
}

func pinGroup(p Pin) int {
	switch p {
	case PinTop:
		return 0
	case PinBottom:
		return 2
	default:
		return 1
	}
}

// Order returns items in Bubble Tea bottom-proximity order:
// PinTop (original order), unranked by Label, ranked oldest-to-newest, PinBottom.
func Order(items []Item) []Item {
	keyed := make([]keyedItem, len(items))
	for i, it := range items {
		keyed[i] = keyedItem{item: it, idx: i}
	}
	slices.SortStableFunc(keyed, compareKeyed)
	out := make([]Item, len(keyed))
	for i, k := range keyed {
		out[i] = k.item
	}
	return out
}

func compareKeyed(a, b keyedItem) int {
	if c := cmp.Compare(pinGroup(a.item.Pin), pinGroup(b.item.Pin)); c != 0 {
		return c
	}
	if a.item.Pin == PinTop || a.item.Pin == PinBottom {
		return cmp.Compare(a.idx, b.idx)
	}
	aRanked := !a.item.Rank.IsZero()
	bRanked := !b.item.Rank.IsZero()
	if aRanked != bRanked {
		if aRanked {
			return 1
		}
		return -1
	}
	if aRanked {
		if c := a.item.Rank.Compare(b.item.Rank); c != 0 {
			return c
		}
	}
	if c := strings.Compare(a.item.Label, b.item.Label); c != 0 {
		return c
	}
	return cmp.Compare(a.idx, b.idx)
}
