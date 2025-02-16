package zns

import (
	"context"
	"fmt"
	"net/http"

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
	OrderNo string
	Amount  string
	TradeNo string
}

type aliPay struct {
	ali *alipay.Client
}

func (p aliPay) NewQR(order Order, notifyURL string) (string, error) {
	r, err := p.ali.TradePreCreate(context.TODO(), alipay.TradePreCreate{
		Trade: alipay.Trade{
			NotifyURL:      notifyURL,
			Subject:        "ZNS Ticket",
			OutTradeNo:     order.OrderNo,
			TotalAmount:    order.Amount,
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
	o.Amount = n.ReceiptAmount

	return
}
