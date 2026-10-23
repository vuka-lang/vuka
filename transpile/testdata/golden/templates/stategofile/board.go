package main

func (b *Board) load() {
	b.rows = []string{"a"}
}

func (b *Board) count() int {
	return len(b.rows.Get())
}

func size(b *Board) int {
	return len(b.rows)
}
