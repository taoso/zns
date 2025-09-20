(() => {
const urlParams = new URLSearchParams(location.search);
let token = urlParams.get('token');
if (token) {
  document.location.href = '//' + document.domain + '/dns/' + token
  return
}

let m = document.location.pathname.match(/^\/dns\/(.+)$/)
token = (m && m[1]) || ""

$ = document.querySelector.bind(document);

$('#pay').onclick = (e) => {
  const y = $('#cents');
  const cents = Math.trunc(y.value * 100);
  if (cents < 100) {
    alert('最低一元钱起购');
    y.focus();
    return;
  }
  fetch('/ticket/?buy=1', {
    method: 'POST',
    headers: {
      'content-type': 'application/json',
    },
    body: JSON.stringify({cents: cents, token: token}),
  }).then((resp) => {
      resp.json().then((d) => {
        let qrcode = new QRCode($('#qr'), { width: 100, height: 100, useSVG: true });
        qrcode.makeCode(d.qr);

        let countdownX;
        let orderLoading = false;
        let countDownDate = new Date().getTime() + (15 * 60 * 1000);
        let updateCountdownX = () => {
          let now = new Date().getTime();
          let distance = countDownDate - now;

          if (distance < 0) {
            clearInterval(countdownX);
            $('#qr').innerHTML = '';
            $('#qr-msg').innerHTML = "订单已失效";
          }

          let minutes = Math.floor((distance % (1000 * 60 * 60)) / (1000 * 60));
          let seconds = Math.floor((distance % (1000 * 60)) / 1000);

          $('#qr-msg').innerHTML = minutes.toString().padStart(2, '0') + ":" + seconds.toString().padStart(2, '0');

          return seconds;
        };
        updateCountdownX();
        countdownX = setInterval(function() {
          let seconds = updateCountdownX();

          if (seconds%5 === 0 && !orderLoading) {
            orderLoading = true;
            fetch(`/ticket/${d.token}`).then((resp) => {
              resp.json().then((tickets) => {
                if (!tickets) return;
                if (tickets[0].buy_order != d.order) return;
                document.location = `/dns/${d.token}`
              });
            }).finally(() => {
                orderLoading = false;
              });;
          }
        }, 1000);
      });
    });
}

fetch(`/ticket/${token}`).then((resp) => {
  resp.json().then((tickets) => {
    if (!tickets) return;
    _ = document.createElement.bind(document);
    t = $('#tickets');
    t.style.display = 'table';

    $('#qr-msg').innerHTML = `你的专属 DoH 链接🔗 <span class="warn">(请勿在互联网上传播!)</span>
    <div class="doh-url">https://${document.domain}/dns/${token}</div>
    <span>备用线路</span><span class="warn">(支持IPv6，延迟较大，主线异常不计流量，主线正常计三倍流量)</span>
    <div class="doh-url">https://us.${document.domain}/dns/${token}</div>
    <div class="help">加电报群 <a href="https://t.me/letszns">t.me/letszns</a> 获取神秘信息🤫</div>`;
    let keyName = {
      "id":          "记录编号",
      "bytes":       "剩余流量",
      "total_bytes": "已购流量",
      "pay_order":   "支付订单",
      "buy_order":   "业务订单",
      "created":     "创建时间",
      "updated":     "更新时间",
      "expires":     "过期时间",
    };
    tickets.forEach((ticket) => {
      let isTime = ["created", "updated", "expires"];
      for ([key, value] of Object.entries(ticket)) {
        let tr = _('tr');
        let th = _('th');
        th.innerText = keyName[key] || key;
        let td = _('td');
        if (isTime.includes(key)) {
          value = new Date(Date.parse(value));
          value = value.toLocaleString();
        }
        td.innerText = value;
        tr.appendChild(th);
        tr.appendChild(td);
        t.appendChild(tr);
      }
      let tr = _('tr');
      tr.appendChild(_('hr'));
      t.appendChild(tr);
    });
  });
});
})()
