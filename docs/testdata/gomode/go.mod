// The Go samples of the docs (::: go blocks) are checked as packages of this
// module, against the libraries at main and this repository's vuka.
module docsamples

go 1.27

replace github.com/vuka-lang/vuka => ../../..

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/a-h/templ v0.3.1020 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/glebarez/go-sqlite v1.21.2 // indirect
	github.com/go-sql-driver/mysql v1.10.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/pgx/v5 v5.10.0 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/vuka-lang/orm v1.0.0 // indirect
	github.com/vuka-lang/ui v1.5.1-0.20261022154417-398d512bd879 // indirect
	github.com/vuka-lang/vuka v1.5.1-0.20261022163744-b9fa93fa1c09 // indirect
	github.com/vuka-lang/web v1.5.1-0.20261022163923-0198b07fd170 // indirect
	golang.org/x/crypto v0.55.0 // indirect
	golang.org/x/mod v0.42.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	golang.org/x/tools v0.52.0 // indirect
	modernc.org/libc v1.22.5 // indirect
	modernc.org/mathutil v1.5.0 // indirect
	modernc.org/memory v1.5.0 // indirect
	modernc.org/sqlite v1.23.1 // indirect
)

tool github.com/vuka-lang/orm/cmd/orm
