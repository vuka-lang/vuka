# Search

Text search (Django's `contrib.postgres.search`), trigram similarity and
vector search (pgvector's), each written for the database the query runs on:

| | Postgres | MySQL | SQLite |
|---|---|---|---|
| text | `tsvector`, `websearch_to_tsquery`, GIN | `MATCH … AGAINST`, `FULLTEXT` | an FTS5 table `<table>_fts`, kept by triggers |
| rank | `ts_rank` | relevance | `-bm25(…)`, the document's weights per column |
| headline | `ts_headline` | a substring about the first word | `snippet()` / `highlight()` |
| trigrams | `pg_trgm` (`%`, `similarity`, GIN/GiST) | `LIKE` | `similarity` in Go, unindexed |
| vectors | pgvector (`<->`, `<=>`, `<#>`, HNSW/IVFFlat) | stored, not searched | distances in Go, unindexed (O(n)) |

So a program developed on SQLite searches the same way on Postgres in
production — with indexes there.

## Declaring

Search attributes on a model's fields declare what migrations make:

```vuka
type Article struct {
	orm.Base
	ID        int64
	Title     string       @orm.Char{Max: 200} @orm.Trigram
	Body      string       @orm.LongText
	Search    orm.TSVector @orm.SearchOf{Fields: []string{"title:A", "body:B"}, Config: "english"} @orm.FullText{Config: "english"}
	Embedding orm.Vector   @orm.Dims(3) @orm.HNSW{Metric: orm.Cosine, M: 16}
}

// MySQL matches a document by a FULLTEXT index of its columns.
func (Article) Indexes() []orm.Index { return []orm.Index{orm.FullTextIndex("title", "body")} }
```

| Attribute | |
|---|---|
| `@orm.SearchOf{Fields, Config}` | on an `orm.TSVector` field: a column generated from the fields, each with its weight (`"title:A"`) |
| `@orm.FullText{Config}` | a full-text index of the field (GIN on Postgres) |
| `@orm.Trigram` | a trigram index (`pg_trgm` GIN on Postgres) |
| `@orm.Dims(n)` | an `orm.Vector`'s dimensions |
| `@orm.HNSW{Metric, M, EfConstruction}`, `@orm.IVFFlat{Metric, Lists}` | a pgvector index; `Metric` is `orm.L2`, `orm.Cosine` or `orm.IP` |

On SQLite a document becomes an external-content FTS5 table (`articles_fts`)
over its fields, with triggers keeping it in step; migrations make and drop
it like an index. An `english` config stems with FTS5's `porter` tokenizer.
A model with no document is searched by `LIKE` on SQLite, unindexed.

The Go spelling is an `Indexes()` method (`orm.GinIndex("search")`,
`orm.FullTextIndex(…)`, `orm.HnswIndex("embedding").Ops(orm.Cosine)`,
`orm.GinIndex("title").Trigram()`) and a `Generated()` method for the
`TSVector` column.

## Text

`orm.Search(fields…)` is a document of field references; `Matches` is its
predicate and `Rank` its relevance, a term to order by:

```vuka
doc := orm.Search(Article.Title, Article.Body)
found := Article.Objects.Filter(doc.Matches("go command")).OrderBy(doc.Rank("go command").Desc()).All(ctx)?
```

The query is web search syntax: every word must match, `"a phrase"` in order,
`or` between alternatives, `-word` must not, and `word*` matches words
starting with it — `go -java "web server" or rust*`. The text is always a
bound argument; on SQLite it is rewritten into an FTS5 query of quoted
phrases, never FTS5 syntax of the user's.

| | |
|---|---|
| `orm.Search(fields…)`, `.Weight("A")`, `.Config("english")`, `.Add(doc)` | a document |
| `doc.Matches(text)`, `doc.MatchesQuery(orm.SearchQuery(text))` | the predicate |
| `doc.Rank(text)`, `doc.RankQuery(q)` | the rank, an `OrderedTerm[T, float64]` |
| `fn.Headline(ref, q)` | an excerpt with the words marked |
| `fn.SearchVector`, `fn.Match`, `fn.SearchRank` | the same, under Django's names |

Where the databases differ:

- **Stopwords.** Postgres's `english` drops its stopwords from queries;
  SQLite drops the same list from an `english` document's queries. MySQL
  drops InnoDB's, and words shorter than `innodb_ft_min_token_size`.
- **Exclusions only.** `-java` alone matches the rows without it on
  Postgres and SQLite; MySQL's boolean mode matches nothing.
- **Ranking.** `ts_rank` weighs A 1, B 0.4, C 0.2, D 0.1; SQLite passes the
  same weights to `bm25`, which also normalises by length.
- **Headlines.** `orm.Marks`, `orm.MaxWords` (at most 64 on SQLite) and
  `orm.HighlightAll` map onto `ts_headline`'s options and onto
  `snippet`/`highlight`.

## Trigrams

`fn.Similarity(ref, text)` is how alike a field is to a text, from 0 to 1 —
for typos and near matches:

```vuka
sim := fn.Similarity(Article.Title, "go tools").As("sim")
like := Article.Objects.Annotate(sim).Filter(sim.Gt(0.3)).OrderBy(sim.Desc()).All(ctx)?
```

## Vectors

`orm.Vector` is a `[]float32` column. `fn.L2Distance`, `fn.CosineDistance`
and `fn.InnerProduct` (negated, as pgvector's `<#>`: smaller is nearer) are
`OrderedTerm[T, float64]`s, so they filter and order:

```vuka
v := orm.Vector{0.9, 0.1, 0}
near := Article.Objects.Filter(fn.CosineDistance(Article.Embedding, v).Lt(0.3)).
	OrderBy(fn.CosineDistance(Article.Embedding, v).Asc()).All(ctx)?
```

`Nearest` is the k-nearest-neighbour query: ordered by distance, nearest
first, annotated `distance`:

```vuka
top := Article.Objects.Nearest(Article.Embedding, v, orm.Cosine).Limit(10).All(ctx)?
```

On Postgres the `ORDER BY` is pgvector's operator, so an HNSW or IVFFlat
index of that metric serves it. The engine's options — `orm.K(n)`,
`orm.MaxDistance(d)`, `orm.DistanceAs(name)`, `orm.EfSearch(n)`
(`hnsw.ef_search`), `orm.Probes(n)` (`ivfflat.probes`), set with `SET LOCAL`
in a transaction of the query's own — are reached through `With`:

```vuka
top = Article.Objects.With(func(qs orm.QuerySet[Article]) orm.QuerySet[Article] {
	return qs.Nearest("embedding", v, orm.K(10), orm.MaxDistance(0.4), orm.EfSearch(100))
}).All(ctx)?
```

MySQL stores vectors but doesn't search them: a `VECTOR(n)` column where the
server has the type (MySQL 9, MariaDB 11.7), else `VARBINARY(4n)` holding the
same little-endian float32s, so an upgraded server converts it in place.
Vector indexes are left out there, and a distance, `Nearest` or `Hybrid`
fails when the query is written, naming the field.

## Hybrid

`Hybrid` ranks rows by a text match and by a vector distance and fuses the
ranks by reciprocal rank (a row scores `w/(k + rank)` per ranking), annotated
`rank`, `distance` and `score`, best first. It runs on Postgres and on SQLite:

```vuka
doc := orm.Search(Article.Title, Article.Body)
best := Article.Objects.With(func(qs orm.QuerySet[Article]) orm.QuerySet[Article] {
	return qs.Hybrid(doc.Document(), orm.SearchQuery("web server"), "embedding", v,
		orm.K(10), orm.Candidates(100), orm.HybridWeights(1, 0.5), orm.RRF(60))
}).All(ctx)?
```

`Candidates(n)` narrows the vector side to the `n` nearest (found by the
index) plus the text's matches. `fn.Fuse(a.Desc(), b.Asc())` is the fusion on
its own, over any two named terms.
