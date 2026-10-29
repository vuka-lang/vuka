package queries

import (
	"fmt"
	"time"

	"github.com/vuka-lang/orm/v2"
)

type Category struct {
	ID    int64
	Name  string
	Links []ProductCategory
}

type ProductCategory struct {
	ID         int64
	CategoryID int64
	Product    *Product
}

type Product struct {
	ID        int64
	Name      string
	Status    string
	RevisedAt time.Time
}

type CategoryFields struct {
	orm.RelationPath
	ID   orm.NumberField[int64]
	Name orm.TextField[string]
}

func (CategoryFields) Links() ProductCategoryFields { return ProductCategoryFields{} }

type ProductCategoryFields struct {
	orm.RelationPath
	ID         orm.NumberField[int64]
	CategoryID orm.NumberField[int64]
}

func (ProductCategoryFields) Product() ProductFields { return ProductFields{} }

type ProductFields struct {
	orm.RelationPath
	ID        orm.NumberField[int64]
	Name      orm.TextField[string]
	Status    orm.TextField[string]
	RevisedAt orm.TimeField
}

func (ProductFields) As(alias string) ProductFields { return ProductFields{} }

var (
	CategoryColumns        CategoryFields
	ProductCategoryColumns ProductCategoryFields
	ProductColumns         ProductFields
)

type CategoryProducts struct {
	ID    int64
	Name  string
	Count int64
}

func target() {
	orm.Objects[Category]().
		Join(ProductCategoryColumns.On(ProductCategoryColumns.CategoryID.EqCol(CategoryColumns.ID)), orm.Left).
		Join(ProductColumns.On(
			ProductCategoryColumns.Product().Eq(ProductColumns.ID),
			ProductColumns.Status.In("active", "featured"),
			ProductColumns.RevisedAt.Gte(orm.Today()),
		), orm.Left).
		OrderBy(CategoryColumns.Name).
		Values[CategoryProducts](CategoryColumns.ID, CategoryColumns.Name, orm.Count(ProductColumns.ID)).
		All(nil)
}

func notJoined() {
	orm.Objects[Category]().
		Filter(ProductColumns.Status.Eq("active")). // want `E0504: Product isn't joined`
		All(nil)
	orm.Objects[Category]().
		Join(ProductCategoryColumns.On(ProductColumns.ID.Eq(1))). // want `E0504: Product is joined after this Join`
		Join(ProductColumns.On(ProductCategoryColumns.Product().Eq(ProductColumns.ID))).
		Values[int64](CategoryColumns.ID).
		All(nil)
	other := ProductColumns.As("other")
	orm.Objects[Product]().
		Values[string](other.Name). // want `E0504: Product "other" isn't joined`
		All(nil)
	orm.Objects[Product]().
		Join(other.On(other.Name.EqCol(ProductColumns.Name))).
		Values[string](other.Name).
		All(nil)
}

func byPosition() {
	orm.Objects[Category]().
		Values[CategoryProducts](CategoryColumns.ID, CategoryColumns.Name). // want `E0505: Values\[CategoryProducts\] fills its 3 fields by position, and 2 columns are given`
		All(nil)
	orm.Objects[Category]().
		Values[CategoryProducts](CategoryColumns.Name, CategoryColumns.ID, orm.Count(CategoryColumns.ID)). // want `E0505: Values\[CategoryProducts\] reads column 1, string, into its field ID, int64` `E0505: Values\[CategoryProducts\] reads column 2, int64, into its field Name, string`
		All(nil)
	orm.Objects[Category]().Values[CategoryProducts]("id", "name").All(nil)
}

func repeated() {
	orm.Objects[Category]().
		Join(CategoryColumns.Links(), orm.Left).
		Join(ProductColumns.On(ProductCategoryColumns.Product().Eq(ProductColumns.ID)), orm.Left).
		Values[CategoryProducts](CategoryColumns.ID, CategoryColumns.Name, orm.Count(CategoryColumns.ID)). // want `W0501: the Join of ProductCategory can repeat the rows this Count counts`
		All(nil)
	orm.Objects[Category]().
		Join(CategoryColumns.Links(), orm.Left).
		Values[CategoryProducts](CategoryColumns.ID, CategoryColumns.Name, orm.Count(CategoryColumns.ID, orm.Distinct)).
		All(nil)
	orm.Objects[Category]().
		Join(CategoryColumns.Links()).
		All(nil) // want `W0502: the Join of ProductCategory can repeat Category's rows`
	orm.Objects[ProductCategory]().
		Join(ProductColumns.On(ProductCategoryColumns.Product().Eq(ProductColumns.ID))).
		All(nil)
}

func fragments(days int, tmpl string) {
	orm.SQLOf[time.Time]("DATE_SUB(CURDATE(), INTERVAL ? DAY)", 3)
	orm.SQLOf[bool]("{0} >= DATE_SUB(CURDATE(), INTERVAL ? DAY)", ProductColumns.RevisedAt, 3)
	orm.SQLOf[bool]("{0} = '?'", ProductColumns.Name)
	orm.SQLOf[int]("? + ?", 1)                                            // want `E0506: the template has 2 \? for 1 arguments no \{n\} names`
	orm.SQLOf[int]("{1} + 1", ProductColumns.ID)                          // want `E0506: the template's \{1\} names no argument: it is given 1`
	orm.SQLOf[int]("LENGTH(" + tmpl + ")")                                // want `E0506: a SQL fragment's template is built with \+`
	orm.SQLOf[int](fmt.Sprintf("INTERVAL %d DAY", days))                  // want `E0506: a SQL fragment's template is built with fmt.Sprintf`
	orm.SQLOf[int](tmpl)                                                  // want `E0506: a SQL fragment's template is built with a variable`
	orm.SQLFor[int](orm.Dialects{orm.MySQL: "? + 1", orm.SQLite: "1"}, 2) // want `E0506: the template has 0 \? for 1 arguments`
	orm.Objects[Category]().
		OrderBy(orm.SQLOf[int]("FIELD({0}, 'featured')", ProductColumns.Status)). // want `E0504: Product isn't joined`
		All(nil)
}

func subquery() {
	orm.Objects[Category]().
		Filter(CategoryColumns.ID.In(1)).
		OrderBy(orm.Objects[Product]().Filter(ProductColumns.Status.Eq("active")).Values[int64](ProductColumns.ID)).
		All(nil)
}
