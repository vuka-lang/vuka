package design

import "strings"

// SkeletonAnimation is how a skeleton's bones move.
type SkeletonAnimation string

const (
	SkeletonPulse SkeletonAnimation = ""     // the bones fade in and out
	SkeletonWave  SkeletonAnimation = "wave" // a highlight sweeps across every bone on screen as one
	SkeletonNone  SkeletonAnimation = "none" // still
)

// animation is the class of a bone moving as a.
func (a SkeletonAnimation) class() string {
	switch a {
	case SkeletonWave:
		return "skeleton-wave"
	case SkeletonNone:
		return ""
	}
	return "skeleton-pulse"
}

// value is a SkeletonOf's data-skeleton-of.
func (a SkeletonAnimation) value() string {
	if a == SkeletonPulse {
		return "pulse"
	}
	return string(a)
}

// skeletonLabel is what a screen reader hears while a skeleton shows.
func skeletonLabel(label string) string {
	if label == "" {
		return "Loading…"
	}
	return label
}

var placeholderWords = strings.Fields("lorem ipsum dolor sit amet consectetur adipiscing elit sed do eiusmod tempor incididunt ut labore et dolore magna aliqua")

// Words is n words of placeholder text, for the data of a component
// SkeletonOf draws: its text bars are as long as the text.
func Words(n int) string {
	w := make([]string, max(n, 0))
	for i := range w {
		w[i] = placeholderWords[i%len(placeholderWords)]
	}
	return strings.Join(w, " ")
}

// Chars is n characters of placeholder text, for a field whose length is
// known (a date, a price, a code).
func Chars(n int) string { return strings.Repeat("x", max(n, 0)) }
