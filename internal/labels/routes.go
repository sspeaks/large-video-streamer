package labels

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sspeaks/large-video-streamer/internal/auth"
	"github.com/sspeaks/large-video-streamer/internal/detect"
)

var labelsPageTemplate = template.Must(template.New("labels-page").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Labels - {{.Show}}</title>
  <style>
    :root { color-scheme: dark; font-family: system-ui, -apple-system, Segoe UI, sans-serif; }
    * { box-sizing: border-box; }
    body { margin: 0; overflow-x: hidden; background: #0f172a; color: #e5e7eb; }
    main { width: min(1100px, 100%); max-width: calc(100vw - 2rem); margin: 0 auto; padding: 1rem; }
    h1, h2 { margin: 1rem 0 .75rem; }
    p { line-height: 1.5; }
    section { margin: 1rem 0; padding: 1rem; border: 1px solid #334155; border-radius: .75rem; background: #111827; }
    .table-wrap { position: relative; max-width: 100%; overflow-x: auto; -webkit-overflow-scrolling: touch; border-radius: .5rem; }
    table { width: 100%; min-width: 44rem; border-collapse: collapse; }
    table.boundary-table { min-width: 42rem; }
    th, td { padding: .6rem; border-bottom: 1px solid #334155; text-align: left; vertical-align: top; }
    th { color: #cbd5e1; font-size: .95rem; }
    input, textarea, select { box-sizing: border-box; width: 100%; min-height: 44px; padding: .65rem; border: 1px solid #475569; border-radius: .4rem; background: #020617; color: #f8fafc; font: inherit; }
    input[type="checkbox"] { width: 1.35rem; min-height: 1.35rem; accent-color: #38bdf8; }
    textarea { min-height: 9rem; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
    button { min-height: 44px; margin: 0; padding: .65rem .85rem; border: 0; border-radius: .45rem; background: #38bdf8; color: #082f49; font-weight: 700; cursor: pointer; display: inline-flex; align-items: center; justify-content: center; gap: .35rem; }
    button.primary { background: #38bdf8; color: #082f49; box-shadow: 0 0 0 1px rgba(125, 211, 252, .25); }
    button.danger { background: #f87171; color: #450a0a; }
    button.secondary { background: #64748b; color: #f8fafc; }
    button:disabled, input:disabled, select:disabled { opacity: .55; cursor: not-allowed; }
    button:focus-visible, input:focus-visible, textarea:focus-visible, select:focus-visible { outline: 3px solid #fde68a; outline-offset: 2px; }
    .actions, .row-actions, .bulk-actions, .candidate-tools, .status-line { display: flex; flex-wrap: wrap; gap: .5rem; align-items: center; }
    .actions { margin: .75rem 0; }
    .row-actions { min-width: 18rem; }
    .candidate-tools, .bulk-actions { margin: .75rem 0; }
    .candidate-tools label, .bulk-actions label { display: inline-flex; gap: .5rem; align-items: center; color: #cbd5e1; }
    .candidate-tools input[type="number"] { width: 7rem; }
    .bulk-actions input, .inline-name { width: 10rem; }
    .status-line { min-height: 1.8rem; }
    .status { min-height: 1.4rem; color: #93c5fd; }
    .save-state { display: inline-flex; align-items: center; min-height: 2rem; padding: .2rem .6rem; border-radius: 999px; font-weight: 700; font-size: .9rem; }
    .save-state.saved { background: #14532d; color: #bbf7d0; }
    .save-state.dirty { background: #713f12; color: #fde68a; }
    .help { margin-top: 0; color: #cbd5e1; }
    details.help { margin: .75rem 0; }
    details.help summary { cursor: pointer; min-height: 44px; display: inline-flex; align-items: center; font-weight: 700; }
    details.help ul { margin: .5rem 0 0; padding-left: 1.2rem; line-height: 1.7; }
    kbd { background: #020617; border: 1px solid #475569; border-radius: .35rem; padding: .05rem .4rem; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: .85em; }
    .field-label { display: block; margin-bottom: .4rem; font-weight: 700; }
    .grid { display: grid; gap: 1rem; grid-template-columns: repeat(auto-fit, minmax(min(22rem, 100%), 1fr)); }
    .empty-state { padding: 1rem; color: #cbd5e1; text-align: center; }
    .sr-only { position: absolute; width: 1px; height: 1px; padding: 0; margin: -1px; overflow: hidden; clip: rect(0, 0, 0, 0); white-space: nowrap; border: 0; }
    video { width: 100%; max-height: 60vh; background: #000; border-radius: .5rem; }
    @media (max-width: 640px) {
      main { max-width: 100%; padding: .75rem; }
      section { padding: .75rem; }
      input, textarea, select { font-size: 16px; }
      .actions button, .candidate-tools label, .bulk-actions label, .bulk-actions button { flex: 1 1 12rem; }
      table { min-width: 46rem; }
      table.boundary-table { min-width: 40rem; }
      .row-actions { gap: .6rem; }
    }
  </style>
</head>
<body>
  <main>
    <h1 id="page-title">Labels for {{.Show}}</h1>
    <p class="status-line">
      <span id="save-state" class="save-state saved">Saved</span>
      <span class="status" id="status" role="status" aria-live="polite"></span>
      <span id="status-actions"></span>
    </p>
    <div class="actions" aria-label="Label editor actions">
      <button id="add-boundary" class="secondary mutating-control">Add boundary</button>
      <button id="save" class="primary mutating-control">Save</button>
      <button id="export" class="secondary mutating-control">Export timestamps</button>
      <button id="import" class="secondary mutating-control">Import timestamps</button>
      <button id="detect" class="secondary mutating-control">Detect silences</button>
    </div>
    <details class="help" id="shortcuts-help">
      <summary>Keyboard shortcuts</summary>
      <ul>
        <li><kbd>s</kbd> — Save</li>
        <li><kbd>a</kbd> — Add boundary</li>
        <li><kbd>d</kbd> — Detect silences</li>
        <li><kbd>j</kbd> — Jump preview to the next boundary</li>
        <li><kbd>k</kbd> — Jump preview to the previous boundary</li>
        <li><kbd>Alt</kbd> + <kbd>↑</kbd> / <kbd>↓</kbd> — Nudge the focused Start time by ±5 s</li>
        <li><kbd>?</kbd> — Show this help</li>
      </ul>
      <p class="help">Single-key shortcuts are ignored while you are typing in a field or the video is focused, so they never interrupt editing or native video controls.</p>
    </details>
    <section>
      <h2>Preview</h2>
      <video id="preview" controls playsinline></video>
      <p class="status" id="previewStatus" role="status" aria-live="polite">Use “Preview” on any boundary or candidate to jump the video to that moment.</p>
    </section>
    <div class="grid">
      <section>
        <h2>Boundaries</h2>
        <div class="table-wrap">
          <table class="boundary-table">
            <thead><tr><th>Name</th><th>Start (HH:MM:SS)</th><th>Actions</th></tr></thead>
            <tbody id="boundaries"></tbody>
          </table>
        </div>
      </section>
      <section>
        <h2>Candidates</h2>
        <p class="help">Review detected silences, then promote useful ones into named boundaries or reject noise.</p>
        <div class="candidate-tools" aria-label="Candidate filters">
          <label for="candidate-sort">Sort
            <select id="candidate-sort">
              <option value="duration-desc">Duration, longest first</option>
              <option value="time-asc">Time, earliest first</option>
            </select>
          </label>
          <label><input type="checkbox" id="hide-handled" checked> Hide promoted/rejected</label>
          <label for="min-duration">Hide shorter than
            <input id="min-duration" type="number" min="0" step="0.1" value="0" inputmode="decimal"> seconds
          </label>
        </div>
        <div class="bulk-actions" aria-label="Bulk candidate actions">
          <label for="bulk-name">Bulk boundary name
            <input id="bulk-name" value="group-a" autocomplete="off">
          </label>
          <button id="bulk-promote" class="secondary mutating-control" disabled>Promote selected</button>
          <button id="bulk-reject" class="danger mutating-control" disabled>Reject selected</button>
        </div>
        <p class="help" id="candidate-count"></p>
        <div class="table-wrap">
          <table>
            <thead><tr><th><input type="checkbox" id="select-all-candidates" aria-label="Select all visible pending candidates"></th><th>Time</th><th>Duration</th><th>Status</th><th>Actions</th></tr></thead>
            <tbody id="candidates"></tbody>
          </table>
        </div>
      </section>
    </div>
    <section>
      <h2>Plain-text timestamps</h2>
      <label class="field-label" for="timestamps">Plain-text boundaries for Export and Import</label>
      <p class="help">Use this box to copy boundaries out or paste edited boundaries back in. The format starts with a video header, then one boundary name and time per line.</p>
      <textarea id="timestamps" placeholder="&gt; {{.Show}}.mkv&#10;group-a 00:02:05"></textarea>
    </section>
  </main>
  <script src="/static/hls.min.js"></script>
  <script>
    const show = {{ .Show }};
    const api = '/labels/api/' + encodeURIComponent(show);
    let labels = { video: show, boundaries: [], candidates: [] };
    let dirty = false;
    let busy = false;
    const selectedCandidates = new Set();
    const statusEl = document.getElementById('status');
    const statusActions = document.getElementById('status-actions');
    const saveStateEl = document.getElementById('save-state');
    const detectButton = document.getElementById('detect');
    const sortControl = document.getElementById('candidate-sort');
    const hideHandledControl = document.getElementById('hide-handled');
    const minDurationControl = document.getElementById('min-duration');
    const selectAllCandidates = document.getElementById('select-all-candidates');
    const bulkName = document.getElementById('bulk-name');
    const bulkPromote = document.getElementById('bulk-promote');
    const bulkReject = document.getElementById('bulk-reject');
    const candidateCount = document.getElementById('candidate-count');
    const displayName = (value) => {
      const words = String(value || '').replace(/[_-]+/g, ' ').replace(/\s+/g, ' ').trim().split(' ').filter(Boolean);
      return words.map((word) => word.charAt(0).toUpperCase() + word.slice(1)).join(' ') || String(value || 'Untitled');
    };
    document.getElementById('page-title').textContent = 'Labels for ' + displayName(show);
    const setStatus = (message) => {
      statusEl.textContent = message || '';
      statusActions.innerHTML = '';
    };
    const updateSaveState = () => {
      saveStateEl.textContent = dirty ? 'Unsaved changes' : 'Saved';
      saveStateEl.className = 'save-state ' + (dirty ? 'dirty' : 'saved');
    };
    const setDirty = (value) => {
      dirty = Boolean(value);
      updateSaveState();
    };
    window.addEventListener('beforeunload', (event) => {
      if (!dirty) return;
      event.preventDefault();
      event.returnValue = '';
    });
    const preview = document.getElementById('preview');
    const previewStatus = document.getElementById('previewStatus');
    const playlistURL = '/hls/' + encodeURIComponent(show) + '/playlist.m3u8';
    (function initPreview() {
      if (window.Hls && Hls.isSupported()) {
        const hls = new Hls({ xhrSetup: (xhr) => { xhr.withCredentials = true; } });
        hls.loadSource(playlistURL);
        hls.attachMedia(preview);
        hls.on(Hls.Events.ERROR, (event, data) => {
          if (data && data.fatal) { previewStatus.textContent = 'Preview unavailable — is this video segmented yet?'; }
        });
      } else if (preview.canPlayType('application/vnd.apple.mpegurl')) {
        preview.src = playlistURL;
      } else {
        previewStatus.textContent = 'This browser cannot play HLS video.';
      }
    })();
    const seekPreview = (seconds) => {
      preview.currentTime = Math.max(0, Number(seconds) || 0);
      preview.play().catch(() => {});
      preview.scrollIntoView({ behavior: 'smooth', block: 'start' });
    };
    const secondsToClock = (seconds) => {
      seconds = Math.max(0, Math.round(Number(seconds) || 0));
      const h = Math.floor(seconds / 3600);
      const m = Math.floor((seconds % 3600) / 60);
      const s = seconds % 60;
      return [h, m, s].map(v => String(v).padStart(2, '0')).join(':');
    };
    const clockToSeconds = (value) => {
      const match = /^([0-9]+):([0-5][0-9]):([0-5][0-9])$/.exec(value.trim());
      if (!match) throw new Error('Invalid time: ' + value);
      return Number(match[1]) * 3600 + Number(match[2]) * 60 + Number(match[3]);
    };
    const escapeText = (value) => String(value).replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
    const escapeAttr = escapeText;
    const normalizeLabels = () => {
      labels = labels || { video: show, boundaries: [], candidates: [] };
      labels.video = labels.video || show;
      labels.boundaries = labels.boundaries || [];
      labels.candidates = labels.candidates || [];
    };
    const candidateStatus = (candidate) => candidate.status || 'candidate';
    const isHandledCandidate = (candidate) => {
      const status = candidateStatus(candidate);
      return status === 'named' || status === 'rejected';
    };
    const candidateKey = (candidate) => String(Number(candidate.time) || 0) + '|' + String(Number(candidate.duration) || 0);
    const candidateItems = () => {
      normalizeLabels();
      const minDuration = Math.max(0, Number(minDurationControl.value) || 0);
      const items = labels.candidates.map((candidate, index) => ({ candidate: candidate, index: index, key: candidateKey(candidate) })).filter((item) => {
        if (hideHandledControl.checked && isHandledCandidate(item.candidate)) return false;
        if ((Number(item.candidate.duration) || 0) < minDuration) return false;
        return true;
      });
      if (sortControl.value === 'duration-desc') {
        items.sort((a, b) => (Number(b.candidate.duration) || 0) - (Number(a.candidate.duration) || 0));
      } else {
        items.sort((a, b) => (Number(a.candidate.time) || 0) - (Number(b.candidate.time) || 0));
      }
      return items;
    };
    const updateCandidateCount = () => {
      const counts = { pending: 0, named: 0, rejected: 0 };
      (labels.candidates || []).forEach((candidate) => {
        const status = candidateStatus(candidate);
        if (status === 'named') counts.named += 1;
        else if (status === 'rejected') counts.rejected += 1;
        else counts.pending += 1;
      });
      candidateCount.textContent = counts.pending + ' pending · ' + counts.named + ' promoted · ' + counts.rejected + ' rejected';
    };
    const selectedVisiblePendingItems = () => candidateItems().filter((item) => selectedCandidates.has(item.key) && !isHandledCandidate(item.candidate));
    const updateBulkControls = () => {
      const selected = selectedVisiblePendingItems();
      bulkPromote.disabled = busy || selected.length === 0;
      bulkReject.disabled = busy || selected.length === 0;
      const visiblePending = candidateItems().filter((item) => !isHandledCandidate(item.candidate));
      selectAllCandidates.disabled = busy || visiblePending.length === 0;
      selectAllCandidates.checked = visiblePending.length > 0 && visiblePending.every((item) => selectedCandidates.has(item.key));
      selectAllCandidates.indeterminate = selected.length > 0 && !selectAllCandidates.checked;
    };
    const setBusy = (value) => {
      busy = Boolean(value);
      document.querySelectorAll('.mutating-control, .editable-control').forEach((control) => { control.disabled = busy; });
      detectButton.textContent = busy ? 'Detecting…' : 'Detect silences';
      updateBulkControls();
    };
    const boundaryNameForBulk = (base, index, total) => total === 1 ? base : base + '-' + String(index + 1);
    const promoteCandidate = (candidate, name) => {
      const cleanName = (name || '').trim();
      if (!cleanName) {
        setStatus('Enter a boundary name before promoting.');
        return false;
      }
      labels.boundaries.push({ name: cleanName, start: Number(candidate.time) || 0 });
      candidate.status = 'named';
      selectedCandidates.delete(candidateKey(candidate));
      setDirty(true);
      return true;
    };
    const rejectCandidate = (candidate) => {
      candidate.status = 'rejected';
      selectedCandidates.delete(candidateKey(candidate));
      setDirty(true);
    };
    const offerUndoDelete = (boundary, index) => {
      statusEl.textContent = 'Deleted boundary “' + (boundary.name || 'unnamed') + '”. Save to persist or undo now.';
      statusActions.innerHTML = '';
      const undo = document.createElement('button');
      undo.type = 'button';
      undo.className = 'secondary mutating-control';
      undo.textContent = 'Undo delete';
      undo.addEventListener('click', () => {
        labels.boundaries.splice(Math.min(index, labels.boundaries.length), 0, boundary);
        setDirty(true);
        render();
        setStatus('Restored boundary “' + (boundary.name || 'unnamed') + '”.');
      });
      statusActions.appendChild(undo);
    };
    const render = () => {
      normalizeLabels();
      const boundaries = document.getElementById('boundaries');
      boundaries.innerHTML = '';
      labels.boundaries.forEach((boundary, index) => {
        const row = document.createElement('tr');
        row.innerHTML = '<td><input class="editable-control" value="' + escapeAttr(boundary.name || '') + '" aria-label="Boundary name"></td><td><input class="editable-control boundary-start" value="' + secondsToClock(boundary.start) + '" aria-label="Boundary start"></td><td><div class="row-actions"><button class="secondary preview">Preview</button><button class="danger delete mutating-control">Delete</button></div></td>';
        const inputs = row.querySelectorAll('input');
        inputs[0].addEventListener('input', () => { boundary.name = inputs[0].value; setDirty(true); });
        inputs[1].addEventListener('input', () => { try { boundary.start = clockToSeconds(inputs[1].value); setDirty(true); setStatus(''); } catch (err) { setStatus(err.message); } });
        inputs[1].addEventListener('keydown', (event) => {
          if (!event.altKey || event.ctrlKey || event.metaKey) return;
          if (event.key !== 'ArrowUp' && event.key !== 'ArrowDown') return;
          event.preventDefault();
          const delta = event.key === 'ArrowUp' ? 5 : -5;
          let current;
          try { current = clockToSeconds(inputs[1].value); } catch (err) { current = Math.max(0, Number(boundary.start) || 0); }
          const next = Math.max(0, current + delta);
          boundary.start = next;
          inputs[1].value = secondsToClock(next);
          setDirty(true);
          setStatus('Nudged start ' + (delta > 0 ? '+5s' : '-5s') + ' to ' + secondsToClock(next) + '. Save to persist.');
        });
        row.querySelector('.preview').addEventListener('click', () => seekPreview(boundary.start));
        row.querySelector('.delete').addEventListener('click', () => {
          const removed = labels.boundaries.splice(index, 1)[0];
          setDirty(true);
          render();
          offerUndoDelete(removed, index);
        });
        boundaries.appendChild(row);
      });
      const candidates = document.getElementById('candidates');
      candidates.innerHTML = '';
      updateCandidateCount();
      const visibleCandidates = candidateItems();
      if (visibleCandidates.length === 0) {
        const row = document.createElement('tr');
        const message = (labels.candidates || []).length === 0 ? 'No candidates yet. Use Detect silences to find possible boundary points.' : 'No candidates match the current filters. Adjust filters or run Detect silences again.';
        row.innerHTML = '<td colspan="5" class="empty-state">' + escapeText(message) + ' <button type="button" class="secondary empty-detect mutating-control">Detect silences</button></td>';
        row.querySelector('.empty-detect').addEventListener('click', runDetect);
        candidates.appendChild(row);
      } else {
        visibleCandidates.forEach((item) => {
          const candidate = item.candidate;
          const row = document.createElement('tr');
          const checked = selectedCandidates.has(item.key) ? ' checked' : '';
          const handled = isHandledCandidate(candidate);
          const disabled = handled ? ' disabled' : '';
          const pendingActions = handled ? '<span class="help">Handled</span>' : '<label><span class="sr-only">Boundary name for candidate at ' + secondsToClock(candidate.time) + '</span><input class="inline-name editable-control" value="group-a" aria-label="Boundary name for candidate at ' + secondsToClock(candidate.time) + '"></label><button class="promote mutating-control">Promote</button><button class="danger reject mutating-control">Reject</button>';
          row.innerHTML = '<td><input type="checkbox" class="candidate-select" aria-label="Select candidate at ' + secondsToClock(candidate.time) + '"' + checked + disabled + '></td><td>' + secondsToClock(candidate.time) + '</td><td>' + Number(candidate.duration || 0).toFixed(2) + 's</td><td>' + escapeText(candidateStatus(candidate)) + '</td><td><div class="row-actions"><button class="secondary preview">Preview</button>' + pendingActions + '</div></td>';
          const checkbox = row.querySelector('.candidate-select');
          checkbox.addEventListener('change', () => {
            if (checkbox.checked) selectedCandidates.add(item.key);
            else selectedCandidates.delete(item.key);
            updateBulkControls();
          });
          row.querySelector('.preview').addEventListener('click', () => seekPreview(candidate.time));
          if (!handled) {
            row.querySelector('.promote').addEventListener('click', () => {
              if (promoteCandidate(candidate, row.querySelector('.inline-name').value)) {
                render();
                setStatus('Promoted candidate at ' + secondsToClock(candidate.time) + '. Save to persist.');
              }
            });
            row.querySelector('.reject').addEventListener('click', () => {
              rejectCandidate(candidate);
              render();
              setStatus('Rejected candidate at ' + secondsToClock(candidate.time) + '. Save to persist.');
            });
          }
          candidates.appendChild(row);
        });
      }
      setBusy(busy);
    };
    const runDetect = async () => {
      if (busy) return;
      setBusy(true);
      setStatus('Detecting silences (analyzing audio)…');
      try {
        const res = await fetch(api + '/detect', { method: 'POST' });
        if (!res.ok) throw new Error(await res.text());
        labels = await res.json();
        normalizeLabels();
        selectedCandidates.clear();
        render();
        const n = labels.candidates.filter(c => candidateStatus(c) === 'candidate').length;
        setDirty(true);
        setStatus('Detected ' + n + ' pending candidate boundary(ies) — promote or reject each, then Save.');
      } catch (err) {
        setStatus('Detect failed: ' + err.message);
      } finally {
        setBusy(false);
      }
    };
    document.getElementById('add-boundary').addEventListener('click', () => {
      labels.boundaries = labels.boundaries || [];
      labels.boundaries.push({ name: 'group-a', start: 0 });
      setDirty(true);
      render();
      setStatus('Added boundary. Save to persist.');
    });
    document.getElementById('save').addEventListener('click', async () => {
      try {
        setStatus('Saving labels…');
        const res = await fetch(api, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(labels) });
        if (!res.ok) throw new Error(await res.text());
        setDirty(false);
        setStatus('Saved labels and chapters.vtt');
      } catch (err) {
        setStatus('Save failed: ' + err.message);
      }
    });
    document.getElementById('export').addEventListener('click', async () => {
      try {
        const res = await fetch(api + '/export');
        if (!res.ok) throw new Error(await res.text());
        document.getElementById('timestamps').value = await res.text();
        setStatus('Exported timestamps');
      } catch (err) {
        setStatus('Export failed: ' + err.message);
      }
    });
    document.getElementById('import').addEventListener('click', async () => {
      try {
        const res = await fetch(api + '/import', { method: 'POST', body: document.getElementById('timestamps').value });
        if (!res.ok) throw new Error(await res.text());
        labels = await res.json();
        normalizeLabels();
        selectedCandidates.clear();
        setDirty(true);
        render();
        setStatus('Imported timestamps and wrote chapters.vtt. Review changes, then Save to mark this page clean.');
      } catch (err) {
        setStatus('Import failed: ' + err.message);
      }
    });
    detectButton.addEventListener('click', runDetect);
    sortControl.addEventListener('change', render);
    hideHandledControl.addEventListener('change', render);
    minDurationControl.addEventListener('input', render);
    selectAllCandidates.addEventListener('change', () => {
      const visiblePending = candidateItems().filter((item) => !isHandledCandidate(item.candidate));
      visiblePending.forEach((item) => {
        if (selectAllCandidates.checked) selectedCandidates.add(item.key);
        else selectedCandidates.delete(item.key);
      });
      render();
    });
    bulkPromote.addEventListener('click', () => {
      const selected = selectedVisiblePendingItems();
      const baseName = (bulkName.value || '').trim();
      if (!baseName) {
        setStatus('Enter a bulk boundary name before promoting.');
        return;
      }
      selected.forEach((item, index) => promoteCandidate(item.candidate, boundaryNameForBulk(baseName, index, selected.length)));
      render();
      setStatus('Promoted ' + selected.length + ' selected candidate(s). Save to persist.');
    });
    bulkReject.addEventListener('click', () => {
      const selected = selectedVisiblePendingItems();
      selected.forEach((item) => rejectCandidate(item.candidate));
      render();
      setStatus('Rejected ' + selected.length + ' selected candidate(s). Save to persist.');
    });
    const jumpBoundary = (direction) => {
      normalizeLabels();
      const starts = labels.boundaries.map((b) => Math.max(0, Number(b.start) || 0)).sort((a, b) => a - b);
      if (starts.length === 0) { setStatus('No boundaries to jump to yet.'); return; }
      const current = Number(preview.currentTime) || 0;
      let target = null;
      if (direction > 0) {
        for (let i = 0; i < starts.length; i++) { if (starts[i] > current + 0.001) { target = starts[i]; break; } }
      } else {
        for (let i = starts.length - 1; i >= 0; i--) { if (starts[i] < current - 0.001) { target = starts[i]; break; } }
      }
      if (target === null) { setStatus(direction > 0 ? 'Already at or past the last boundary.' : 'Already at or before the first boundary.'); return; }
      seekPreview(target);
      setStatus('Jumped to boundary at ' + secondsToClock(target) + '.');
    };
    const openShortcutsHelp = () => {
      const help = document.getElementById('shortcuts-help');
      if (help) { help.open = true; help.scrollIntoView({ behavior: 'smooth', block: 'start' }); }
    };
    const isTypingTarget = (el) => {
      if (!el) return false;
      if (el.isContentEditable) return true;
      const tag = el.tagName;
      return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || tag === 'VIDEO';
    };
    document.addEventListener('keydown', (event) => {
      if (event.defaultPrevented) return;
      if (event.ctrlKey || event.altKey || event.metaKey) return;
      if (isTypingTarget(document.activeElement)) return;
      const key = event.key.length === 1 ? event.key.toLowerCase() : event.key;
      let handled = true;
      switch (key) {
        case 's': document.getElementById('save').click(); break;
        case 'a': document.getElementById('add-boundary').click(); break;
        case 'd': runDetect(); break;
        case 'j': jumpBoundary(1); break;
        case 'k': jumpBoundary(-1); break;
        case '?': openShortcutsHelp(); break;
        default: handled = false;
      }
      if (handled) event.preventDefault();
    });
    (async () => {
      try {
        const res = await fetch(api);
        if (!res.ok) throw new Error(await res.text());
        labels = await res.json();
        normalizeLabels();
        render();
        setDirty(false);
      } catch (err) { setStatus(err.message); }
    })();
  </script>
</body>
</html>`))

// RegisterRoutes wires the label UI and JSON API endpoints into mux.
func (s *Store) RegisterRoutes(mux *http.ServeMux, a *auth.Authenticator) {
	mux.Handle("GET /labels/{show}", a.RequirePage(http.HandlerFunc(s.handleLabelsPage)))
	mux.Handle("GET /labels/api/{show}", a.RequireMedia(http.HandlerFunc(s.handleLabelsGet)))
	mux.Handle("POST /labels/api/{show}", a.RequireMedia(http.HandlerFunc(s.handleLabelsPost)))
	mux.Handle("POST /labels/api/{show}/import", a.RequireMedia(http.HandlerFunc(s.handleLabelsImport)))
	mux.Handle("GET /labels/api/{show}/export", a.RequireMedia(http.HandlerFunc(s.handleLabelsExport)))
	mux.Handle("POST /labels/api/{show}/mkv/import", a.RequireMedia(http.HandlerFunc(s.handleMKVImport)))
	mux.Handle("POST /labels/api/{show}/mkv/embed", a.RequireMedia(http.HandlerFunc(s.handleMKVEmbed)))
	mux.Handle("POST /labels/api/{show}/detect", a.RequireMedia(http.HandlerFunc(s.handleDetect)))
}

func (s *Store) handleLabelsPage(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := labelsPageTemplate.Execute(w, struct{ Show string }{Show: show}); err != nil {
		http.Error(w, "failed to render labels page", http.StatusInternalServerError)
	}
}

func (s *Store) handleLabelsGet(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	labels, err := s.Load(show)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

func (s *Store) handleLabelsPost(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	defer r.Body.Close()
	var labels VideoLabels
	if err := json.NewDecoder(r.Body).Decode(&labels); err != nil {
		http.Error(w, "invalid labels JSON", http.StatusBadRequest)
		return
	}
	labels.Video = show
	if err := validateBoundaryNames(labels.Boundaries); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.saveAndWriteChapters(labels); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Store) handleLabelsImport(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	defer r.Body.Close()
	labels, err := s.ImportTimestamps(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	labels.Video = show
	if err := validateBoundaryNames(labels.Boundaries); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.saveAndWriteChapters(labels); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

func (s *Store) handleLabelsExport(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	labels, err := s.Load(show)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, s.ExportTimestamps(labels))
}

func (s *Store) handleMKVImport(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	labels, err := s.Load(show)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	boundaries, err := s.ImportMKVChapters(filepath.Join(s.cfg.VideoDir, show+".mkv"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	labels.Boundaries = sortedBoundaries(append(labels.Boundaries, boundaries...))
	labels.Video = show
	if err := s.saveAndWriteChapters(labels); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

func (s *Store) handleMKVEmbed(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	labels, err := s.Load(show)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := s.ExportMKVChapters(filepath.Join(s.cfg.VideoDir, show+".mkv"), labels.Boundaries); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDetect runs ffmpeg silencedetect over the source .mkv and merges the
// resulting candidate boundaries into the show's labels. It preserves any
// existing user decisions (candidates already promoted/rejected) and only adds
// newly detected times that don't coincide with a kept candidate. Candidates do
// not affect chapters.vtt until promoted to boundaries, so we only Save here.
func (s *Store) handleDetect(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	labels, err := s.Load(show)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	silences, err := detect.DetectSilence(filepath.Join(s.cfg.VideoDir, show+".mkv"), detect.DefaultNoiseDB, detect.DefaultMinDur)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	detected := make([]Candidate, 0, len(silences))
	for _, sil := range silences {
		detected = append(detected, Candidate{Time: sil.Time, Duration: sil.Duration, Status: "candidate"})
	}
	labels.Candidates = mergeCandidates(labels.Candidates, detected)
	labels.Video = show
	if err := s.Save(labels); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

// mergeCandidates keeps candidates the user has already decided on
// (status "named" or "rejected") and adds freshly detected candidates whose
// time is not within one second of a kept one. The result is sorted by time.
func mergeCandidates(existing, detected []Candidate) []Candidate {
	var kept []Candidate
	for _, c := range existing {
		if c.Status == "named" || c.Status == "rejected" {
			kept = append(kept, c)
		}
	}
	result := append([]Candidate(nil), kept...)
	for _, d := range detected {
		duplicate := false
		for _, k := range kept {
			if math.Abs(k.Time-d.Time) < 1.0 {
				duplicate = true
				break
			}
		}
		if !duplicate {
			result = append(result, d)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Time < result[j].Time })
	return result
}

func validShowFromRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	show := r.PathValue("show")
	if err := validateShowName(show); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return "", false
	}
	return show, true
}

func validateShowName(show string) error {
	if show == "" {
		return errors.New("show is required")
	}
	if strings.Contains(show, "/") || strings.Contains(show, `\`) || strings.Contains(show, "..") || filepath.Base(show) != show {
		return errors.New("invalid show name")
	}
	return nil
}

func validateBoundaryNames(boundaries []Boundary) error {
	for _, boundary := range boundaries {
		if strings.ContainsAny(boundary.Name, "\r\n") {
			return errors.New("boundary names cannot contain line breaks")
		}
	}
	return nil
}

func (s *Store) saveAndWriteChapters(labels VideoLabels) error {
	if err := s.Save(labels); err != nil {
		return err
	}
	dir := filepath.Join(s.cfg.HLSDir, labels.Video)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "chapters.vtt"), []byte(s.ToWebVTT(labels)), 0o644)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func sortBoundariesInPlace(boundaries []Boundary) {
	sort.SliceStable(boundaries, func(i, j int) bool {
		return boundaries[i].Start < boundaries[j].Start
	})
}
