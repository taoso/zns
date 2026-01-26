package zns

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-kiss/sqlx"
	_ "modernc.org/sqlite"
)

type Ticket struct {
	ID       int    `db:"id" json:"id"`
	Token    string `db:"token" json:"-"`
	Cents    int    `db:"cents" json:"cents"`
	Tier     int    `db:"tier" json:"tier"`
	PayOrder string `db:"pay_order" json:"pay_order"` // 商户订单号
	BuyOrder string `db:"buy_order" json:"buy_order"` // 支付宝交易号

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
	cents INTEGER,
	tier INTEGER,
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
	// 新增购买记录
	NewTicket(ticket Ticket) error
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

type sqliteTicketReop struct {
	db *sqlx.DB
}

func (r sqliteTicketReop) Init() {
	if _, err := r.db.Exec((*Ticket).Schema(nil)); err != nil {
		panic(err)
	}
}

func (r sqliteTicketReop) NewTicket(t Ticket) error {
	now := time.Now()

	t.ID = 0
	t.Created = now
	t.Updated = now

	oldTickets := []Ticket{}
	sql := "select * from " + (*Ticket).TableName(nil) +
		" where token = ? and tier = ? order by id desc"
	err := r.db.Select(&oldTickets, sql, t.Token, t.Tier)
	if err != nil {
		return err
	}

	// 普通会员一元30天有效期，高级会员三元30天有效期
	p := float64(t.Cents) / 100.0
	if t.Tier == 1 {
		p /= 3.0
	}

	d := time.Duration(30*p) * 24 * time.Hour

	if len(oldTickets) > 0 && oldTickets[0].Expires.After(now) {
		t.Expires = oldTickets[0].Expires.Add(d)
	} else {
		t.Expires = now.Add(d)
	}

	_, err = r.db.Insert(&t)
	return err
}

func (r sqliteTicketReop) List(token string, limit int) (tickets []Ticket, err error) {
	sql := "select * from " + (*Ticket).TableName(nil) +
		" where token = ? order by id desc limit ?"
	err = r.db.Select(&tickets, sql, token, limit)
	return
}

type TicketHandler struct {
	Pay    Pay
	Repo   TicketRepo
	AltSvc string

	Nodes []Node
}

func (h *TicketHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/my-nodes/") {
		h.GetMyNodes(w, r)
		return
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
			Tier  int    `json:"tier"`
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
		o := Order{
			OrderNo: req.Token + "@" + now,
			CentNum: req.Cents,
			Tier:    req.Tier,
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

		err = h.Repo.NewTicket(Ticket{
			Token:    token,
			Cents:    o.CentNum,
			Tier:     o.Tier,
			PayOrder: o.OrderNo,
			BuyOrder: o.TradeNo,
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Write([]byte("success"))
	}
}

type Node struct {
	Host string `json:"host" yaml:"host"`
	Flag string `json:"flag" yaml:"flag"`
	IPv6 bool   `json:"ipv6" yaml:"ipv6"`
	Tier int    `json:"tier" yaml:"tier"`
}

func (h *TicketHandler) GetMyNodes(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")

	tickets, err := h.Repo.List(token, 10)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	tier := -1
	now := time.Now()

	for _, t := range tickets {
		if t.Expires.After(now) {
			if t.Tier > tier {
				tier = t.Tier
			}
		}
	}

	nodes := []Node{}

	for _, n := range h.Nodes {
		if n.Tier <= tier {
			nodes = append(nodes, n)
		}
	}

	w.Header().Add("content-type", "application/json")
	json.NewEncoder(w).Encode(nodes)
}
