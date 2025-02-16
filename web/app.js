const urlParams = new URLSearchParams(location.search);
const token = urlParams.get('token');

$ = document.querySelector.bind(document);

if (document.location.hostname == 'zns.nu.mk') {
  document.location.hostname = 'zns.lehu.in';
}

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
                document.location = `/?token=${d.token}`
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

    $('#qr-msg').innerHTML = `DoH 🔗 https://${document.domain}/dns/${token}`;
    tickets.forEach((ticket) => {
      let isTime = ["created", "updated", "expires"];
      for ([key, value] of Object.entries(ticket)) {
        let tr = _('tr');
        let th = _('th');
        th.innerText = key;
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
    });
  });
});
