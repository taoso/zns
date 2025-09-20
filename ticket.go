package zns

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-kiss/sqlx"
	"modernc.org/sqlite"
)

type Ticket struct {
	ID         int    `db:"id" json:"id"`
	Token      string `db:"token" json:"-"`
	Bytes      int    `db:"bytes" json:"bytes"`
	TotalBytes int    `db:"total_bytes" json:"total_bytes"`
	PayOrder   string `db:"pay_order" json:"pay_order"`
	BuyOrder   string `db:"buy_order" json:"buy_order"`

	Created time.Time `db:"created" json:"created"`
	Updated time.Time `db:"updated" json:"updated"`
	Expires time.Time `db:"expires" json:"expires"`
}

func (_ *Ticket) KeyName() string   { return "id" }
func (_ *Ticket) TableName() string { return "tickets" }
func (t *Ticket) Schema() string {
	return "CREATE TABLE IF NOT EXISTS " + t.TableName() + `(
	` + t.KeyName() + ` INTEGER PRIMARY KEY AUTOINCREMENT,
	token TEXT,
	bytes INTEGER,
	total_bytes INTEGER,
	pay_order TEXT,
	buy_order TEXT,
	created DATETIME,
	updated DATETIME,
	expires DATETIME
);
	CREATE INDEX IF NOT EXISTS t_token_expires ON ` + t.TableName() + `(token, expires);
	CREATE UNIQUE INDEX IF NOT EXISTS t_pay_order ON ` + t.TableName() + `(pay_order);`
}

type TicketRepo interface {
	// New create and save one Ticket
	New(token string, bytes int, trade string, order string) error
	// Cost decreases  bytes of one Ticket
	Cost(token string, bytes int) error
	// List fetches all current Tickets with bytes available.
	List(token string, limit int) ([]Ticket, error)
}

func NewTicketRepo(path string) TicketRepo {
	db, err := sqlx.Connect("sqlite", path)
	if err != nil {
		panic(err)
	}
	db.SetMaxOpenConns(1)
	r := sqliteTicketReop{db: db}
	r.Init()
	return r
}

type FreeTicketRepo struct{}

func (r FreeTicketRepo) New(token string, bytes int, trade, order string) error {
	return nil
}

func (r FreeTicketRepo) Cost(token string, bytes int) error {
	return nil
}

func (r FreeTicketRepo) List(token string, limit int) ([]Ticket, error) {
	return []Ticket{{Bytes: 100}}, nil
}

type sqliteTicketReop struct {
	db *sqlx.DB
}

func (r sqliteTicketReop) Init() {
	if _, err := r.db.Exec((*Ticket).Schema(nil)); err != nil {
		panic(err)
	}
}

func (r sqliteTicketReop) New(token string, bytes int, trade, order string) error {
	now := time.Now()

	ts, err := r.List(token, 1)
	if err != nil {
		return err
	}

	t := Ticket{
		Token:      token,
		Bytes:      bytes,
		TotalBytes: bytes,
		PayOrder:   order,
		BuyOrder:   trade,
		Created:    now,
		Updated:    now,
	}

	gb := bytes / 1024 / 1024 / 1024
	if gb == 0 {
		gb = 1
	}
	d := 30 * 24 * time.Hour * time.Duration(gb)
	if len(ts) == 1 && ts[0].Expires.After(now) {
		t.Expires = ts[0].Expires.Add(d)
	} else {
		t.Expires = now.Add(d)
	}

	_, err = r.db.Insert(&t)

	se := &sqlite.Error{}
	// constraint failed: UNIQUE constraint failed
	if errors.As(err, &se) && se.Code() == 2067 {
		return nil
	}

	return err
}

func (r sqliteTicketReop) Cost(token string, bytes int) error {
	now := time.Now()

	sql := "update " + (*Ticket).TableName(nil) +
		" set bytes = bytes - ?, updated = ? " +
		" where id in (select id from " + (*Ticket).TableName(nil) +
		" where token = ? and expires > ? order by id asc limit 1) and bytes >= ?"

	_r, err := r.db.Exec(sql, bytes, now, token, now, bytes)
	if err != nil {
		return err
	}
	n, err := _r.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}

	return r.costSlow(token, bytes)
}

func (r sqliteTicketReop) costSlow(token string, bytes int) error {
	q := "select * from " + (*Ticket).TableName(nil) +
		" where token = ? and bytes > 0 and expires > ?" +
		" order by id asc"
	var ts []Ticket
	if err := r.db.Select(&ts, q, token, time.Now()); err != nil {
		return err
	}

	if len(ts) == 0 {
		return sql.ErrNoRows
	}

	var i int
	var t Ticket
	for i, t = range ts {
		if t.Bytes >= bytes {
			ts[i].Bytes -= bytes
			bytes = 0
			break
		} else {
			bytes -= t.Bytes
			ts[i].Bytes = 0
		}
	}

	if bytes > 0 {
		ts[i].Bytes -= bytes
	}

	if i == 0 {
		t := ts[i]
		t.Updated = time.Now()
		_, err := r.db.Update(&t)
		return err
	}

	tx, err := r.db.Beginx()
	if err != nil {
		return err
	}

	for ; i >= 0; i-- {
		t := ts[i]
		t.Updated = time.Now()
		_, err := tx.Update(&t)
		if err != nil {
			tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func (r sqliteTicketReop) List(token string, limit int) (tickets []Ticket, err error) {
	sql := "select * from " + (*Ticket).TableName(nil) +
		" where token = ? order by id desc limit ?"
	err = r.db.Select(&tickets, sql, token, limit)
	return
}

type TicketHandler struct {
	MBpCNY int
	Pay    Pay
	Repo   TicketRepo
	AltSvc string
}

func (h *TicketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.AltSvc != "" {
		w.Header().Set("Alt-Svc", h.AltSvc)
	}

	if r.Method == http.MethodGet {
		token := r.PathValue("token")
		ts, err := h.Repo.List(token, 10)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Add("content-type", "application/json")
		json.NewEncoder(w).Encode(ts)
		return
	}

	if r.URL.Query().Get("buy") != "" {
		req := struct {
			Token string `json:"token"`
			Cents int    `json:"cents"`
		}{}
		defer r.Body.Close()
		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if req.Cents < 100 {
			http.Error(w, "cents must > 100", http.StatusBadRequest)
			return
		}

		if req.Token == "" {
			b := make([]byte, 16)
			_, err := rand.Read(b)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			req.Token = base64.RawURLEncoding.EncodeToString(b)
			// 兼容 token 在域名中的情形
			req.Token = strings.ToLower(req.Token)
			req.Token = strings.ReplaceAll(req.Token, "-", "z")
			req.Token = strings.ReplaceAll(req.Token, "_", "z")
		}

		now := time.Now().Format(time.RFC3339)
		yuan := strconv.FormatFloat(float64(req.Cents)/100, 'f', 2, 64)
		o := Order{
			OrderNo: req.Token + "@" + now,
			Amount:  yuan,
		}

		notify := "https://" + r.Host + r.URL.Path
		qr, err := h.Pay.NewQR(o, notify)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Add("content-type", "application/json")
		json.NewEncoder(w).Encode(struct {
			QR    string `json:"qr"`
			Token string `json:"token"`
			Order string `json:"order"`
		}{QR: qr, Token: req.Token, Order: o.OrderNo})
	} else {
		o, err := h.Pay.OnPay(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		i := strings.Index(o.OrderNo, "@")
		token := o.OrderNo[:i]

		yuan, err := strconv.ParseFloat(o.Amount, 64)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		bytes := int(yuan * float64(h.MBpCNY) * 1024 * 1024)

		err = h.Repo.New(token, bytes, o.OrderNo, o.TradeNo)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Write([]byte("success"))
	}
}
