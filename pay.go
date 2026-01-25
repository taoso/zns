package zns

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/smartwalle/alipay/v3"
)

type Pay interface {
	NewQR(order Order, notifyURL string) (string, error)
	OnPay(req *http.Request) (Order, error)
}

func NewPay(appID, privateKey, publicKey string) Pay {
	client, err := alipay.New(appID, privateKey, true)
	if err != nil {
		panic(err)
	}

	if err = client.LoadAliPayPublicKey(publicKey); err != nil {
		panic(err)
	}

	return aliPay{ali: client}
}

type Order struct {
	CentNum int    // 支付金额，单位分
	OrderNo string // 商户订单号
	TradeNo string // 支付宝交易号
	Tier    int    // 购买的套餐等级
}

type aliPay struct {
	ali *alipay.Client
}

func (p aliPay) NewQR(order Order, notifyURL string) (string, error) {
	extras := url.Values{}
	extras.Add("tier", strconv.Itoa(order.Tier))
	r, err := p.ali.TradePreCreate(context.TODO(), alipay.TradePreCreate{
		Trade: alipay.Trade{
			NotifyURL:      notifyURL,
			Subject:        "ZNS Ticket",
			OutTradeNo:     order.OrderNo,
			PassbackParams: extras.Encode(),
			TotalAmount:    strconv.FormatFloat(float64(order.CentNum)/100, 'f', 2, 64),
			TimeoutExpress: "15m",
		},
	})
	if err != nil {
		return "", err
	}

	if r.Code != alipay.CodeSuccess {
		return "", fmt.Errorf("TradePreCreate error: %w", err)
	}

	return r.QRCode, nil
}

func (p aliPay) OnPay(req *http.Request) (o Order, err error) {
	n, err := p.ali.GetTradeNotification(req)
	if err != nil {
		return
	}

	o.OrderNo = n.OutTradeNo
	o.TradeNo = n.TradeNo

	f, err := strconv.ParseFloat(n.ReceiptAmount, 64)
	if err != nil {
		return
	}

	o.CentNum = int(f * 100)
	return
}

func NewZzPay(znsPrivateKey, zzPublicKey string) Pay {
	zzkey, err := base64.RawURLEncoding.DecodeString(zzPublicKey)
	if err != nil {
		panic(err)
	}

	seed, err := base64.RawURLEncoding.DecodeString(znsPrivateKey)
	if err != nil {
		panic(err)
	}

	znsKey := ed25519.NewKeyFromSeed(seed)

	return &zzPay{
		zzPublicKey:   zzkey,
		znsPrivateKey: znsKey,
	}
}

type zzOrderArgs struct {
	CentNum int       `json:"cent_num"` // 支付金额，单位是分
	OrderID string    `json:"order_id"` // 业务订单号
	Subject string    `json:"subject"`  // 订单标题
	Extra   string    `json:"extra"`    // 业务扩展参数
	Created time.Time `json:"created"`  // 请求时间

	NotifyURL string `json:"notify_url,omitempty"` // 异步通知地址
}

type zzPay struct {
	zzPublicKey   []byte
	znsPrivateKey ed25519.PrivateKey
}

func (p zzPay) NewQR(order Order, notifyURL string) (qr string, err error) {
	now := time.Now()

	extras := url.Values{}
	extras.Add("tier", strconv.Itoa(order.Tier))

	args := zzOrderArgs{
		CentNum:   order.CentNum,
		OrderID:   order.OrderNo,
		Subject:   "ZNS Ticket",
		Extra:     extras.Encode(),
		Created:   now,
		NotifyURL: notifyURL,
	}

	body, err := json.Marshal(args)
	if err != nil {
		return
	}

	sign := ed25519.Sign(p.znsPrivateKey, body)

	req, err := http.NewRequest("POST", "https://lehu.in/+/alipay-order-create", bytes.NewReader(body))
	if err != nil {
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("zz-app", "zns")
	req.Header.Set("zz-sign", base64.RawURLEncoding.EncodeToString(sign))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var data struct {
		QR  string `json:"qr"`
		TTL int    `json:"ttl"`
	}

	if err = json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return
	}

	qr = data.QR

	return
}

func (p zzPay) OnPay(req *http.Request) (o Order, err error) {
	defer req.Body.Close()

	body, err := io.ReadAll(req.Body)
	if err != nil {
		return
	}

	sign, err := base64.RawURLEncoding.DecodeString(req.Header.Get("zz-sign"))
	if err != nil {
		return
	}

	if !ed25519.Verify(p.zzPublicKey, body, sign) {
		err = fmt.Errorf("invalid signature")
		return
	}

	args := zzOrderArgs{}
	if err = json.Unmarshal(body, &args); err != nil {
		return
	}

	extras, err := url.ParseQuery(args.Extra)
	if err != nil {
		return
	}

	o.OrderNo = args.OrderID
	o.CentNum = args.CentNum
	o.TradeNo = extras.Get("trade_no")

	e, _ := url.QueryUnescape(extras.Get("extra"))
	q, _ := url.ParseQuery(e)

	o.Tier, _ = strconv.Atoi(q.Get("tier"))

	return
}
