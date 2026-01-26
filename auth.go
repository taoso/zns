package zns

import "time"

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
