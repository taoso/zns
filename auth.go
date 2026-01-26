package zns

import (
	"log"
	"time"

	"github.com/BurntSushi/toml"
)

type Author interface {
	Auth(token string) (bool, error)
}

type DBAuthor struct {
	Repo TicketRepo
}

func (a DBAuthor) Auth(token string) (ok bool, err error) {
	ts, err := a.Repo.List(token, 1)
	if err != nil {
		return
	}

	if len(ts) == 0 || ts[0].Expires.Before(time.Now()) {
		ok = false
		return
	}

	return true, nil
}

type FileAuther struct {
	Path string

	tokens map[string]bool
}

func (a *FileAuther) Load() error {
	tokens := map[string]bool{}
	_, err := toml.DecodeFile(a.Path, &tokens)
	if err == nil {
		log.Println("Loaded tokens: ", len(tokens))
		a.tokens = tokens
	}
	return err
}

func (a *FileAuther) Auth(token string) (ok bool, err error) {
	return a.tokens[token], nil
}
