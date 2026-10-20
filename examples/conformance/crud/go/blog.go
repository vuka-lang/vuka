package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/vuka-lang/orm"
	"github.com/vuka-lang/vuka"
	"github.com/vuka-lang/web"
)

// Blog is the blog's service: its database, opened in memory and seeded.
type Blog struct{}

func NewBlog() (*Blog, error) {
	ctx := context.Background()
	db, err := orm.ConnectURL("sqlite::memory:")
	if err != nil {
		return nil, err
	}
	orm.AddDatabase("main", db, orm.AsDefault())
	if err := orm.CreateTables(ctx, orm.Objects[Author](), orm.Objects[Post]()); err != nil {
		return nil, err
	}
	for _, name := range []string{"Ada", "Grace"} {
		a := Author{Name: name}
		if err := a.Save(ctx); err != nil {
			return nil, err
		}
	}
	return &Blog{}, nil
}

// audit is what the audited functions did, in order.
var audit struct {
	sync.Mutex
	lines []string
}

// audited is a call decorator: each call's name and error.
func audited(c *vuka.Call) {
	c.Next()
	audit.Lock()
	defer audit.Unlock()
	audit.lines = append(audit.lines, fmt.Sprintf("%s %v", c.Name, c.Err()))
}

// NewPost is a post to write.
type NewPost struct {
	Author int64  `json:"author" validate:"required"`
	Title  string `json:"title" validate:"required,max=200"`
}

// createPost writes a post and counts it on its author, in one transaction.
var createPost = vuka.Wrap(func(ctx context.Context, in NewPost) (p Post, err error) {
	err = orm.Transaction(ctx, func(ctx context.Context) error {
		a, err := orm.Objects[Author]().Get(ctx, AuthorFields.ID.Eq(in.Author))
		if err != nil {
			return err
		}
		p = Post{Author: orm.FKID[Author](a.ID), Title: strings.TrimSpace(in.Title)}
		if err := p.Save(ctx); err != nil {
			return err
		}
		if _, err := orm.Objects[Author]().Filter(AuthorFields.ID.Eq(a.ID)).Update(ctx, orm.Set{"posts": AuthorFields.Posts.Add(1)}); err != nil {
			return err
		}
		if strings.HasPrefix(p.Title, "!") {
			return web.BadRequest("a title can't start with !")
		}
		return nil
	})
	return p, err
}, vuka.FuncInfo{Name: "main.createPost"}, audited)

func find(ctx context.Context, id int64) (Post, error) {
	p, err := orm.Objects[Post]().Get(ctx, PostFields.ID.Eq(id))
	if errors.Is(err, orm.ErrNotFound) {
		return p, web.NotFound
	}
	return p, err
}

// ListAuthors answers GET /authors.
func ListAuthors(ctx context.Context, b *Blog) ([]Author, error) {
	return orm.Objects[Author]().OrderBy(AuthorFields.Name.Asc()).All(ctx)
}

// ListPosts answers GET /posts: ?author= one author's, ?live= the published.
func ListPosts(ctx context.Context, author string, live bool, b *Blog) ([]Post, error) {
	qs := orm.Objects[Post]().QuerySet
	if live {
		qs = PostLive()
	}
	if author != "" {
		qs = qs.Filter(PostFields.Author.Name.Eq(author))
	}
	return qs.All(ctx)
}

// ShowPost answers GET /posts/{id}.
func ShowPost(ctx context.Context, id int64, b *Blog) (Post, error) { return find(ctx, id) }

// FindPost answers GET /find?q=: the first post titled q in any case, or
// by the author named q.
func FindPost(ctx context.Context, q string, b *Blog) (Post, error) {
	f := PostFields
	p, err := orm.Objects[Post]().Filter(orm.Or(orm.Lower(f.Title).Eq(strings.ToLower(q)), f.Author.Name.Eq(q))).First(ctx)
	if errors.Is(err, orm.ErrNotFound) {
		return p, web.NotFound
	}
	return p, err
}

// CreatePost answers POST /posts.
func CreatePost(ctx context.Context, in NewPost, b *Blog) (Post, error) { return createPost(ctx, in) }

// ViewPost counts a read: views + 1, in the database.
var ViewPost = vuka.Wrap(func(ctx context.Context, id int64, b *Blog) (Post, error) {
	if _, err := orm.Objects[Post]().Filter(PostFields.ID.Eq(id)).Update(ctx, orm.Set{"views": PostFields.Views.Add(1)}); err != nil {
		return Post{}, err
	}
	return find(ctx, id)
}, vuka.FuncInfo{Name: "main.ViewPost"}, audited)

// PublishPost publishes a post, writing that field alone.
func PublishPost(ctx context.Context, id int64, b *Blog) (Post, error) {
	p, err := find(ctx, id)
	if err != nil {
		return Post{}, err
	}
	p.Published = true
	if err := p.SaveFields(ctx, PostFields.Published); err != nil {
		return Post{}, err
	}
	return p, nil
}

// Audit answers GET /audit: what the audited functions did.
func Audit() []string {
	audit.Lock()
	defer audit.Unlock()
	return append([]string{}, audit.lines...)
}
