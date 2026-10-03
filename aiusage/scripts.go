package aiusage

// 補助 WebView のページ内で動く取得スクリプト。ClaudeMonitor (cc10dda) の auth_webview.go /
// codex_auth_webview.go から、内容を変えずに移植している。

// timedFetchJS は両プロバイダの取得スクリプトが使う共通ヘルパ。
//
// ページ内の fetch が返らないと、Go 側は authView.refresh のタイムアウトで一律
// "fetch timeout" にするしかなく、どこで止まったのかが残らない。fetch ごとに
// 打ち切り、何が起きたかを文言に含めて返す。
//
// 各スクリプトの ms の合計は authView.refresh の待ち時間より短く保つこと。
// 超えると Go 側のタイムアウトが先に走り、せっかくの理由が表示に出ない。
const timedFetchJS = `
  function timedFetch(url, label, ms, init) {
    const ac = new AbortController();
    const timer = setTimeout(function() { ac.abort(); }, ms);
    const opts = Object.assign({credentials: 'include', signal: ac.signal}, init || {});
    return fetch(url, opts).then(function(r) {
      clearTimeout(timer);
      return r;
    }, function(e) {
      clearTimeout(timer);
      if (e && e.name === 'AbortError') throw new Error(label + ' が ' + ms + 'ms で応答しません');
      throw new Error(label + ' に失敗: ' + String((e && e.message) || e));
    });
  }
`

// 取得の合計上限: 使用量 8s + bootstrap 4s = 12s
const authFetcherScript = `
(function() {` + timedFetchJS + `
  // explicit は Go から明示的に呼ばれたかどうか。初期化スクリプトはドキュメント生成毎に
  // 走るので、ナビゲーション途中の about:blank などで定期タイマーが発火することがある。
  // そこで報告すると直前の正常な値をエラーで上書きしてしまうため、報告は explicit のみ。
  async function fetchClaudeUsage(explicit) {
    if (location.hostname !== 'claude.ai') {
      if (explicit) {
        window.__postFetchError && window.__postFetchError('claude.ai を表示していません (現在: ' + location.hostname + ')');
      }
      return;
    }
    try {
      const orgMatch = document.cookie.match(/lastActiveOrg=([^;]+)/);
      if (!orgMatch) {
        // 通常はログインページにリダイレクトされている状態
        window.__postLoginRequired && window.__postLoginRequired('lastActiveOrg cookie 未取得 (未ログイン)');
        return;
      }
      const orgId = decodeURIComponent(orgMatch[1]);
      const headers = {'Content-Type': 'application/json'};
      // 使用量を先に確定させる。以前は bootstrap と Promise.all で束ねていたため、
      // 補助情報でしかない bootstrap が返らないだけで使用量まで巻き添えで止まっていた。
      const usageR = await timedFetch('/api/organizations/' + orgId + '/usage', '使用量 API', 8000, {headers});
      if (usageR.status === 401 || usageR.status === 403) {
        window.__postLoginRequired && window.__postLoginRequired('Cookie 失効 (status=' + usageR.status + ')');
        return;
      }
      if (!usageR.ok) {
        window.__postFetchError && window.__postFetchError('usage fetch failed: status=' + usageR.status);
        return;
      }
      const usage = await usageR.json();
      // ここから先は表示名・プラン等の補助情報。失敗しても使用量は返す。
      let email = '', display = '', caps = [], tier = '', billing = '';
      try {
        const bootR = await timedFetch('/edge-api/bootstrap/' + orgId + '/app_start?statsig_hashing_algorithm=djb2&growthbook_format=sdk&include_system_prompts=false', 'bootstrap API', 4000, {headers});
        if (bootR.ok) {
          const boot = await bootR.json();
          email = (boot.account && boot.account.email_address) || '';
          display = (boot.account && (boot.account.display_name || boot.account.full_name)) || '';
          const memberships = (boot.account && boot.account.memberships) || [];
          const m = memberships.find(function(x) { return x && x.organization && x.organization.uuid === orgId; });
          if (m && m.organization) {
            caps = m.organization.capabilities || [];
            tier = m.organization.rate_limit_tier || '';
            billing = m.organization.billing_type || '';
          }
        }
      } catch (e) { /* bootstrap は取得失敗・パース失敗とも致命傷ではない */ }

      // 追加使用量（従量課金 / extra_usage）を抽出する。
      // /api/organizations/{orgId}/usage の extra_usage は次の構造:
      //   {is_enabled, monthly_limit, used_credits, utilization, currency}
      // ・used_credits / monthly_limit はセント単位 (347 = $3.47)
      // ・monthly_limit が 0 / null の場合は「上限なし（無制限）」
      // ・resets_at フィールドは無いため月初リセットを Go 側で計算する
      let overage = null;
      const eu = usage.extra_usage;
      if (eu && typeof eu === 'object' && eu.is_enabled && typeof eu.used_credits === 'number') {
        const amount = eu.used_credits / 100;
        let limit = null;
        if (typeof eu.monthly_limit === 'number' && eu.monthly_limit > 0) {
          limit = eu.monthly_limit / 100;
        }
        // API はリセット日を返さないため、月初 (UTC) を計算で入れる。
        const now = new Date();
        const reset = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() + 1, 1)).toISOString();
        overage = {amountUsed: amount, spendingLimit: limit, resetsAt: reset};
      }

      const payload = {
        fiveHour: usage.five_hour ? {utilization: usage.five_hour.utilization, resetsAt: usage.five_hour.resets_at} : null,
        sevenDay: usage.seven_day ? {utilization: usage.seven_day.utilization, resetsAt: usage.seven_day.resets_at} : null,
        overage: overage,
        email: email,
        displayName: display,
        capabilities: caps,
        rateLimitTier: tier,
        billingType: billing
      };
      window.__postUsageData && window.__postUsageData(JSON.stringify(payload));
    } catch (e) {
      window.__postFetchError && window.__postFetchError(String((e && e.message) || e));
    }
  }
  // Go から呼ばれる経路だけ explicit=true。無言で return すると Go 側は 15 秒待って
  // "fetch timeout" にしかできないので、この経路では必ず理由を返す。
  window.__fetchClaudeUsage = function() { return fetchClaudeUsage(true); };
  // ナビゲーション直後はリダイレクトと cookie 反映を待つために 1.5s 遅延
  setTimeout(function() { fetchClaudeUsage(false); }, 1500);
  // バックアップタイマー (Go 側 ticker と二重保険)。
  // まず安全な既定値で起動し、直後に Go へ最新の設定間隔を問い合わせて上書きする。
  // この Init はドキュメント生成（再ナビゲーション含む）毎に走るため、再ログイン後も
  // 常に最新値へ収束する。設定変更時は Go が __setUsageInterval(ms) を Eval で呼ぶ。
  let __usageTimer = setInterval(function() { fetchClaudeUsage(false); }, 5 * 60 * 1000);
  window.__setUsageInterval = function(ms) {
    if (__usageTimer) clearInterval(__usageTimer);
    __usageTimer = setInterval(function() { fetchClaudeUsage(false); }, ms);
  };
  if (window.__getUsageIntervalMs) {
    window.__getUsageIntervalMs().then(function(ms) {
      if (ms > 0) window.__setUsageInterval(ms);
    }).catch(function() {});
  }
})();
`

// 取得の合計上限: session 5s + usage 6s + me 3s = 14s
const codexAuthFetcherScript = `
(function() {` + timedFetchJS + `
  function decodeJwtPayload(token) {
    try {
      const part = token.split('.')[1];
      if (!part) return {};
      const base64 = part.replace(/-/g, '+').replace(/_/g, '/');
      const padded = base64 + '='.repeat((4 - base64.length % 4) % 4);
      return JSON.parse(decodeURIComponent(Array.prototype.map.call(atob(padded), function(c) {
        return '%' + ('00' + c.charCodeAt(0).toString(16)).slice(-2);
      }).join('')));
    } catch (_) {
      return {};
    }
  }

  // explicit の意味は Claude 側と同じ。定期タイマー由来では報告しない。
  async function fetchCodexUsage(explicit) {
    if (location.hostname !== 'chatgpt.com') {
      if (explicit) {
        window.__postCodexFetchError && window.__postCodexFetchError('chatgpt.com を表示していません (現在: ' + location.hostname + ')');
      }
      return;
    }
    try {
      // ChatGPT のログインCookieからWebセッションを取得する。wham APIはCookieだけでなく
      // Bearer tokenを要求するため、セッションのaccessTokenを同一オリジン内で利用する。
      const sessionR = await timedFetch('/api/auth/session', 'セッション API', 5000, {headers: {'Accept': 'application/json'}});
      if (sessionR.status === 401 || sessionR.status === 403) {
        window.__postCodexLoginRequired && window.__postCodexLoginRequired('ChatGPT にログインしてください');
        return;
      }
      if (!sessionR.ok) {
        window.__postCodexFetchError && window.__postCodexFetchError('session fetch failed: status=' + sessionR.status);
        return;
      }
      const session = await sessionR.json();
      const accessToken = session.accessToken || session.access_token || '';
      if (!accessToken) {
        window.__postCodexLoginRequired && window.__postCodexLoginRequired('ChatGPT セッションを取得できません。再ログインしてください');
        return;
      }
      const claims = decodeJwtPayload(accessToken);
      const authClaims = claims['https://api.openai.com/auth'] || {};
      const accountId =
        (session.account && session.account.id) || session.accountId || session.account_id ||
        authClaims.chatgpt_account_id || authClaims.account_id || '';
      const usageHeaders = {'Accept': 'application/json', 'Authorization': 'Bearer ' + accessToken};
      if (accountId) usageHeaders['ChatGPT-Account-Id'] = accountId;
      const usageR = await timedFetch('/backend-api/wham/usage', '使用量 API', 6000, {headers: usageHeaders});
      if (usageR.status === 401 || usageR.status === 403) {
        window.__postCodexLoginRequired && window.__postCodexLoginRequired('ChatGPT の認証期限が切れています。再ログインしてください');
        return;
      }
      if (!usageR.ok) {
        window.__postCodexFetchError && window.__postCodexFetchError('usage fetch failed: status=' + usageR.status);
        return;
      }
      const usage = await usageR.json();
      let email = (session.user && session.user.email) || claims.email || '';
      let displayName = (session.user && session.user.name) || claims.name || '';
      try {
        const meR = await timedFetch('/backend-api/me', 'me API', 3000, {headers: {'Accept': 'application/json'}});
        if (meR.ok) {
          const me = await meR.json();
          email = email || me.email || (me.user && me.user.email) || '';
          displayName = displayName || me.name || (me.user && me.user.name) || '';
        }
      } catch (_) {}
      window.__postCodexUsageData && window.__postCodexUsageData(JSON.stringify({usage, email, displayName}));
    } catch (e) {
      window.__postCodexFetchError && window.__postCodexFetchError(String((e && e.message) || e));
    }
  }
  window.__fetchCodexUsage = function() { return fetchCodexUsage(true); };
  setTimeout(function() { fetchCodexUsage(false); }, 1500);
  let __codexUsageTimer = setInterval(function() { fetchCodexUsage(false); }, 5 * 60 * 1000);
  window.__setCodexUsageInterval = function(ms) {
    if (__codexUsageTimer) clearInterval(__codexUsageTimer);
    __codexUsageTimer = setInterval(function() { fetchCodexUsage(false); }, ms);
  };
  if (window.__getCodexUsageIntervalMs) {
    window.__getCodexUsageIntervalMs().then(function(ms) {
      if (ms > 0) window.__setCodexUsageInterval(ms);
    }).catch(function() {});
  }
})();
`
