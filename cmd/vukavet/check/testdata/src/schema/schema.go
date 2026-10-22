package schema

import "github.com/vuka-lang/orm/v2"

type Stamps struct{ Created int }

type Meta struct{ Note string }

type Post struct {
	Stamps
	*Meta
	ID     int
	Title  string
	Author *Author
	Tags   [2]string
}

type Author struct{ Name string }

var other Post

func (p *Post) Schema(s *orm.Schema) {
	s.Model(orm.Meta{Table: "posts"}, nil) // want `E0502: s.Model is given a nil attribute`
	s.Field(&p.Title, orm.Char{Max: 200})
	s.Field(&p.Created)
	s.Field(&p.Stamps.Created)
	s.Field(&(*p).ID)
	s.Field(p.Title)        // want `E0501: s.Field is given string, not the address of one of Post's fields`
	s.Field(&other.Title)   // want `E0501: s.Field is given &other.Title, which isn't a field of Post`
	s.Field(&p.Tags[0])     // want `E0501: s.Field is given &p.Tags\[0\], which isn't a field of Post`
	s.Field(&p.Note)        // want `E0503: s.Field is given &p.Note, behind the embedded pointer Meta`
	s.Field(&p.Author.Name) // want `E0503: s.Field is given &p.Author.Name, behind the pointer p.Author`
	s.Field(&p.ID, nil)     // want `E0502: s.Field\(&p.ID, …\) is given a nil attribute`
	attrs := []orm.FieldAttr{nil}
	s.Field(&p.ID, attrs...)
}

type Tag struct{ Name string }

func (t Tag) Schema(s *orm.Schema) { // want `E0501: Tag's Schema has a value receiver`
	s.Field(&t.Name)
}
