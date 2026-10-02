module github.com/harmonia-vault/workspace

go 1.26.4

require github.com/harmonia-vault/core-go v0.0.0

require (
	github.com/Microsoft/go-winio v0.6.2 // indirect
	golang.org/x/crypto v0.53.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

replace github.com/harmonia-vault/core-go => ./core-go
