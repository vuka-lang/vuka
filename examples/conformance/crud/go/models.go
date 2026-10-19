package main

import (
	"github.com/vuka-lang/orm"
	_ "github.com/vuka-lang/orm/sqlite"
)

// Author writes posts; Posts counts them.
type Author struct {
	orm.Base[Author]
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Posts int    `json:"posts"`
}

func (a *Author) Schema(s *orm.Schema) {
	s.Field(&a.Name, orm.Char{Max: 50, Unique: true})
}

// Post is an author's post.
type Post struct {
	orm.Base[Post]
	ID        int64          `json:"id"`
	Author    orm.FK[Author] `json:"author"`
	Title     string         `json:"title"`
	Views     int            `json:"views"`
	Published bool           `json:"published"`
}

func (p *Post) Schema(s *orm.Schema) {
	s.Model(orm.Meta{Table: "posts", Ordering: []string{"id"}})
	s.Field(&p.Author, orm.Rel{Related: "posts"})
	s.Field(&p.Title, orm.Char{Max: 200})
}

// PostLive is the published posts.
func PostLive() orm.QuerySet[Post] { return orm.Objects[Post]().Filter(PostFields.Published.Eq(true)) }
