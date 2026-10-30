        // ============ 勤務時間タイマー ============
        // 表示値は Go 側（worktimer.go）が計算して /api/system の WorkTimer に載せる。ここは描画と操作だけ。
        // 2 秒周期の更新で入力欄を作り直さないよう、DOM は固定で、中身だけ書き換える。
        let lastWork = null;
        let workEditing = false;
        let workErrTimer = 0;
        let workLayoutKey = '';
        let workEditInitial = '';
        function fmtWorkMin(m){ return Math.floor(m / 60) + ':' + String(m % 60).padStart(2, '0'); }
        function showWorkErr(msg){
            const el = document.getElementById('work-err');
            el.textContent = msg; el.hidden = !msg;
            clearTimeout(workErrTimer);
            if (msg) workErrTimer = setTimeout(function(){ showWorkErr(''); }, 4000);
            workHooks.heightChanged();
        }
        function applyWorkTimer(info){
            if (!info) return;   // 非表示のときは null。区画は applyHidden が隠す
            lastWork = info;
            const idle = info.state === 'idle';
            const cls = idle ? 'idle' : (info.paused ? 'paused' : info.level);   // 昼休み中は黄/赤より優先して灰色
            const worked = idle ? '-:--' : fmtWorkMin(info.workedMin);
            const floored = idle ? '-:--' : fmtWorkMin(info.flooredMin);
            const head = document.getElementById('work-head');
            head.textContent = info.showFloor ? worked + ' (' + floored + ')' : worked;
            head.className = 'section-value ' + cls;
            document.getElementById('work-cap').textContent = info.showFloor ? '実勤務時間 (15分切り下げ)' : '実勤務時間';
            const big = document.getElementById('work-big');
            big.className = 'wk-big ' + cls;
            big.textContent = worked;
            if (info.showFloor) {
                const small = document.createElement('small');
                small.textContent = ' (' + floored + ')';
                big.appendChild(small);
            }
            const bar = document.getElementById('work-bar');
            bar.className = 'progress-fill work-fill ' + cls;
            bar.style.width = (idle || !info.targetMin ? 0 : Math.min(100, info.workedMin / info.targetMin * 100)) + '%';

            // 開始（編集中は触らない）
            const note = document.getElementById('work-note');
            if (!workEditing) {
                document.getElementById('work-start').textContent = (info.start || '--:--') + ' ✎';
                document.getElementById('work-nudge').hidden = info.state !== 'running';
                if (!idle && info.pressed && info.adjustMin !== 0) {
                    note.textContent = '押下 ' + info.pressed + ' ' + (info.adjustMin > 0 ? '−' : '+') + ' 補正 ' + Math.abs(info.adjustMin) + '分';
                    note.hidden = false;
                } else note.hidden = true;
            }
            document.getElementById('work-target').textContent = fmtWorkMin(info.targetMin);
            document.getElementById('work-lunch').textContent = info.lunchStart + '–' + info.lunchEnd;
            const chip = document.getElementById('work-chip');
            if (info.paused) { chip.textContent = '🍴 昼休み中（' + info.lunchEnd + ' まで停止）'; chip.hidden = false; }
            else if (info.state === 'ended') { chip.textContent = '⏹ 終了 ' + info.end; chip.hidden = false; }
            else chip.hidden = true;
            const startBtn = document.getElementById('work-btn-start');
            startBtn.textContent = '勤務開始' + (info.startLagMin > 0 ? '（' + info.startLagMin + '分前から）' : '');
            startBtn.hidden = info.state !== 'idle';
            document.getElementById('work-btn-stop').hidden = info.state !== 'running';
            document.getElementById('work-btn-reset').hidden = info.state === 'idle';

            // 行の増減で高さが変わるので、構成が変わったときだけ測り直す
            const key = [info.state, info.paused, !note.hidden, !chip.hidden, workEditing].join('|');
            if (key !== workLayoutKey) {
                const first = workLayoutKey === '';
                workLayoutKey = key;
                if (!first) workHooks.heightChanged();
            }
        }
        // 操作は POST。応答は操作後のスナップショットなので、次の周期を待たずに描く。
        async function workAction(action, value){
            try {
                const q = '/api/worktimer?action=' + action + (value === undefined ? '' : '&value=' + encodeURIComponent(value));
                const res = await apiFetch(q, { method: 'POST' });
                if (!res.ok) { showWorkErr((await res.text()).trim()); return false; }
                showWorkErr('');
                applyWorkTimer(await res.json());
                return true;
            } catch (e) { showWorkErr('通信に失敗しました'); return false; }
        }
        function setWorkEditing(on){
            workEditing = on;
            const input = document.getElementById('work-start-input');
            document.getElementById('work-start').hidden = on;
            input.hidden = !on;
            if (on) {
                document.getElementById('work-nudge').hidden = true;
                const d = new Date();
                input.value = (lastWork && lastWork.start) || (String(d.getHours()).padStart(2, '0') + ':' + String(d.getMinutes()).padStart(2, '0'));
                workEditInitial = input.value;
                input.focus();
            }
            if (lastWork) applyWorkTimer(lastWork);
        }
        let workCommitting = false;
        // fromBlur: フォーカスが外れての確定。失敗したら編集を閉じて元の表示に戻し、エラーだけを残す
        // （フォーカスのない入力欄を残さない）。Enter での確定は、失敗したら欄を残して打ち直せるようにする。
        async function commitWorkStart(fromBlur){
            const input = document.getElementById('work-start-input');
            if (workCommitting || !input.value) return;
            workCommitting = true;
            try {
                const ok = await workAction('setstart', input.value);
                if (ok || fromBlur) setWorkEditing(false);
            } finally { workCommitting = false; }
        }
        document.getElementById('workSection').addEventListener('click', function(e){
            const t = e.target.closest('[data-wk]');
            if (!t) return;
            e.stopPropagation();
            switch (t.dataset.wk) {
                case 'start': workAction('start'); break;
                case 'stop': workAction('stop'); break;
                case 'reset': workAction('reset'); break;
                case 'nudge-': workAction('nudge', '-1'); break;
                case 'nudge+': workAction('nudge', '1'); break;
                case 'edit': showWorkErr(''); setWorkEditing(true); break;
            }
        });
        (function(){
            const input = document.getElementById('work-start-input');
            // <input type="time"> は、時の 1 桁を打っただけで値が有効になり change が発火する（打ちかけで確定してしまう）。
            // そのため確定は Enter と、値を変えてフォーカスが外れたときだけにする。Escape で取り消す。
            input.addEventListener('keydown', function(e){
                e.stopPropagation();
                if (e.key === 'Enter') { e.preventDefault(); commitWorkStart(false); }
                else if (e.key === 'Escape') { showWorkErr(''); setWorkEditing(false); }
            });
            input.addEventListener('blur', function(){
                if (!workEditing || workCommitting) return;
                if (input.value && input.value !== workEditInitial) commitWorkStart(true);
                else setWorkEditing(false);
            });
        })();
        // 別のウィンドウ（本体 / ポップアップ）での操作を、次の周期を待たずに反映する。
        // updateData() は呼ばない（Network / Disk のグラフ履歴に同じサンプルが二重に積まれるため）。
        window.__workChanged = function(){
            apiFetch('/api/system').then(function(r){ return r.json(); }).then(function(d){ applyWorkTimer(d.WorkTimer); }).catch(function(){});
        };
