(function() {
    var hasPush = ('serviceWorker' in navigator) && ('PushManager' in window);
    var isIOS = /iPad|iPhone|iPod/.test(navigator.userAgent) ||
        (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
    var isStandalone = window.navigator.standalone === true ||
        window.matchMedia('(display-mode: standalone)').matches;
    var iosSafariNeedsPWA = isIOS && !isStandalone && !hasPush;

    // On non-iOS, require Push API support. On iOS Safari (not PWA), we still
    // show the button but intercept the click to show install instructions.
    if (!iosSafariNeedsPWA && (!('serviceWorker' in navigator) || !('PushManager' in window))) return;

    var desktopBtn = document.getElementById('push-subscribe-btn');
    var mapBtn = document.getElementById('map-push-btn');
    var buttons = [desktopBtn, mapBtn].filter(function(b) { return b; });
    if (buttons.length === 0) return;

    var token = (desktopBtn || mapBtn).dataset.token;
    if (!token) return;

    buttons.forEach(function(b) { b.style.display = ''; });
    var swReg;

    if (!iosSafariNeedsPWA) {
        navigator.serviceWorker.register('/service-worker.js').then(function(reg) {
            swReg = reg;
            return reg.pushManager.getSubscription();
        }).then(function(sub) {
            updateButtons(sub);
        }).catch(function(err) {
            console.error('SW registration failed:', err);
        });
    }

    buttons.forEach(function(b) {
        b.addEventListener('click', function() {
            if (iosSafariNeedsPWA) {
                showIOSInstallPrompt();
                return;
            }
            if (b.dataset.subscribed === 'true') {
                unsubscribe();
            } else {
                subscribe();
            }
        });
    });

    function subscribe() {
        fetch('/t/' + token + '/push/vapid-public-key')
            .then(function(r) {
                if (!r.ok) throw new Error('Push not available');
                return r.json();
            })
            .then(function(data) {
                return swReg.pushManager.subscribe({
                    userVisibleOnly: true,
                    applicationServerKey: urlBase64ToUint8Array(data.publicKey)
                });
            })
            .then(function(sub) {
                var key = sub.getKey('p256dh');
                var auth = sub.getKey('auth');
                return fetch('/t/' + token + '/push/subscribe', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({
                        endpoint: sub.endpoint,
                        keys: {
                            p256dh: arrayBufferToBase64Url(key),
                            auth: arrayBufferToBase64Url(auth)
                        }
                    })
                }).then(function() { updateButtons(sub); });
            })
            .catch(function(err) {
                if (err.name === 'NotAllowedError') {
                    alert('Notification permission was denied. Please enable it in your browser settings.');
                } else {
                    console.error('Push subscribe failed:', err);
                }
            });
    }

    function unsubscribe() {
        swReg.pushManager.getSubscription().then(function(sub) {
            if (!sub) return;
            var endpoint = sub.endpoint;
            sub.unsubscribe().then(function() {
                return fetch('/t/' + token + '/push/unsubscribe', {
                    method: 'POST',
                    headers: { 'Content-Type': 'application/json' },
                    body: JSON.stringify({ endpoint: endpoint })
                });
            }).then(function() { updateButtons(null); });
        });
    }

    function updateButtons(sub) {
        buttons.forEach(function(btn) {
            var isMapBubble = btn.classList.contains('map-push-bubble');
            if (sub) {
                btn.dataset.subscribed = 'true';
                btn.title = 'Stop notifications for this trip';
                if (isMapBubble) {
                    btn.innerHTML = '<i class="icon-bell-off"></i>';
                    btn.classList.add('map-push-bubble-active');
                } else {
                    btn.innerHTML = '<i class="icon-bell-off"></i> Unsubscribe';
                }
            } else {
                btn.dataset.subscribed = 'false';
                btn.title = 'Get notified when new entries are posted';
                if (isMapBubble) {
                    btn.innerHTML = '<i class="icon-bell"></i>';
                    btn.classList.remove('map-push-bubble-active');
                } else {
                    btn.innerHTML = '<i class="icon-bell"></i> Notify me';
                }
            }
        });
    }

    // iOS "Add to Home Screen" install prompt
    function showIOSInstallPrompt() {
        // Don't show if already visible
        if (document.getElementById('ios-install-prompt')) return;

        var overlay = document.createElement('div');
        overlay.id = 'ios-install-prompt';
        overlay.innerHTML =
            '<div class="ios-prompt-backdrop"></div>' +
            '<div class="ios-prompt-sheet">' +
                '<div class="ios-prompt-content">' +
                    '<div class="ios-prompt-icon"><i class="icon-bell"></i></div>' +
                    '<p class="ios-prompt-title">Enable notifications</p>' +
                    '<p class="ios-prompt-text">To receive notifications on iPhone, you need to add this page to your Home Screen first.</p>' +
                    '<div class="ios-prompt-steps">' +
                        '<div class="ios-prompt-step">' +
                            '<span class="ios-prompt-step-num">1</span>' +
                            '<span>Tap the <strong>Share</strong> button</span>' +
                            '<svg class="ios-prompt-share-icon" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M4 12v8a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-8"/><polyline points="16 6 12 2 8 6"/><line x1="12" y1="2" x2="12" y2="15"/></svg>' +
                        '</div>' +
                        '<div class="ios-prompt-step">' +
                            '<span class="ios-prompt-step-num">2</span>' +
                            '<span>Tap <strong>Add to Home Screen</strong></span>' +
                        '</div>' +
                        '<div class="ios-prompt-step">' +
                            '<span class="ios-prompt-step-num">3</span>' +
                            '<span>Open from Home Screen and tap the bell again</span>' +
                        '</div>' +
                    '</div>' +
                '</div>' +
                '<button class="ios-prompt-close">Got it</button>' +
                '<div class="ios-prompt-arrow">' +
                    '<svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="6 9 12 15 18 9"/></svg>' +
                '</div>' +
            '</div>';

        document.body.appendChild(overlay);

        overlay.querySelector('.ios-prompt-backdrop').addEventListener('click', closePrompt);
        overlay.querySelector('.ios-prompt-close').addEventListener('click', closePrompt);

        // Animate in
        requestAnimationFrame(function() {
            overlay.classList.add('ios-prompt-visible');
        });

        function closePrompt() {
            overlay.classList.remove('ios-prompt-visible');
            setTimeout(function() { overlay.remove(); }, 300);
        }
    }

    function urlBase64ToUint8Array(base64String) {
        var padding = '='.repeat((4 - base64String.length % 4) % 4);
        var base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
        var raw = atob(base64);
        var arr = new Uint8Array(raw.length);
        for (var i = 0; i < raw.length; i++) arr[i] = raw.charCodeAt(i);
        return arr;
    }

    function arrayBufferToBase64Url(buffer) {
        var bytes = new Uint8Array(buffer);
        var binary = '';
        for (var i = 0; i < bytes.byteLength; i++) {
            binary += String.fromCharCode(bytes[i]);
        }
        return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    }
})();
