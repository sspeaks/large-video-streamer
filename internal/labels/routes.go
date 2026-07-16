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
)

var labelsPageTemplate = template.Must(template.New("labels-page").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Labels - {{.Show}}</title>
  <link rel="stylesheet" href="/static/app.css">
  <style>
    * { box-sizing: border-box; }
    body { overflow-x: hidden; }
    main { width: min(1100px, calc(100% - 2rem)); margin: 0 auto; padding: 1rem 0 2rem; }
    header { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; margin: 1rem 0; }
    h1, h2 { margin: 0 0 .75rem; }
    h1 { font-size: clamp(2rem, 5vw, 3.5rem); letter-spacing: -0.05em; }
    p { line-height: 1.5; }
    section { margin: 1rem 0; padding: 1rem; border: 1px solid var(--border); border-radius: var(--radius); background: var(--surface); box-shadow: var(--shadow); }
    .table-wrap { position: relative; max-width: 100%; overflow-x: auto; -webkit-overflow-scrolling: touch; border-radius: .5rem; }
    .candidate-table-wrap { max-height: 60vh; overflow: auto; border: 1px solid var(--border); }
    .candidate-table-wrap thead th { position: sticky; top: 0; z-index: 1; background: var(--surface); }
    table { width: 100%; min-width: 44rem; border-collapse: collapse; }
    table.boundary-table { min-width: 42rem; }
    table.candidate-table { min-width: 58rem; }
    input, textarea, select { padding: .65rem; }
    textarea { min-height: 9rem; font-family: var(--font-mono); }
    button { min-height: 44px; margin: 0; padding: .65rem .85rem; border: 1px solid transparent; border-radius: var(--radius-sm); background: var(--accent-solid); color: var(--accent-solid-text); font: inherit; font-weight: 700; cursor: pointer; display: inline-flex; align-items: center; justify-content: center; gap: .35rem; white-space: nowrap; }
    button:hover { filter: brightness(1.06); }
    button.primary { background: var(--accent-solid); color: var(--accent-solid-text); border-color: var(--accent-solid); }
    button.danger { background: var(--danger); color: var(--danger-text); border-color: var(--danger); }
    button.secondary { background: var(--secondary-bg); color: var(--text); border-color: var(--secondary-border); }
    button.secondary:hover { border-color: var(--accent); }
    button:disabled, input:disabled, select:disabled { opacity: .55; cursor: not-allowed; }
    .actions, .row-actions, .bulk-actions, .candidate-tools, .status-line, .nav, .header-actions { display: flex; flex-wrap: wrap; gap: .5rem; align-items: center; }
    .nav { gap: .6rem; margin-bottom: .85rem; }
    .actions { margin: .75rem 0; }
    .row-actions { min-width: 18rem; }
    .candidate-table .row-actions { min-width: 15rem; }
    .candidate-tools, .bulk-actions, .autodetect-options { margin: .75rem 0; }
    .candidate-tools label, .bulk-actions label, .autodetect-options label { display: inline-flex; gap: .5rem; align-items: center; color: var(--text-muted); }
    .candidate-tools input[type="number"] { width: 7rem; }
    .bulk-actions input, .inline-name { width: 10rem; }
    .autodetect-panel textarea { width: 100%; min-height: 6rem; }
    .source-badges { display: flex; flex-wrap: wrap; gap: .25rem; }
    .source-badge, .conflict-badge, .low-confidence-badge { display: inline-flex; align-items: center; min-height: 1.5rem; padding: .1rem .45rem; border-radius: 999px; font-size: .85em; font-weight: 700; }
    .source-badge { background: var(--surface-input); color: var(--text); border: 1px solid var(--border); }
    .source-badge--black { background: #111827; color: #f9fafb; border-color: #6b7280; }
    .source-badge--freeze { background: #dbeafe; color: #172554; border-color: #93c5fd; }
    .conflict-badge { background: var(--danger); color: var(--danger-text); }
    .low-confidence-badge { background: #facc15; color: #422006; }
    .status-line { min-height: 1.8rem; }
    .status { min-height: 1.4rem; color: var(--accent); }
    .help { margin-top: 0; color: var(--text-muted); }
    details.help { margin: .75rem 0; }
    details.help summary { cursor: pointer; min-height: 44px; display: inline-flex; align-items: center; font-weight: 700; }
    details.help ul { margin: .5rem 0 0; padding-left: 1.2rem; line-height: 1.7; }
    kbd { background: var(--surface-input); border: 1px solid var(--border-strong); border-radius: .35rem; padding: .05rem .4rem; font-family: var(--font-mono); font-size: .85em; }
    .field-label { display: block; margin-bottom: .4rem; font-weight: 700; }
    .grid { display: grid; gap: 1rem; grid-template-columns: minmax(0, 1fr); }
    .empty-state { padding: 1rem; color: var(--text-muted); text-align: center; }
    tr.candidate-conflict > td { box-shadow: inset 3px 0 0 var(--danger); }
    tr.candidate-low-confidence > td { box-shadow: inset 3px 0 0 #facc15; }
    tr.candidate-conflict.candidate-low-confidence > td { box-shadow: inset 3px 0 0 var(--danger), inset 6px 0 0 #facc15; }
    tr.candidate-current > td { background: var(--surface-input); }
    tr.candidate-current { outline: 2px solid var(--accent); outline-offset: -2px; }
    video { width: 100%; max-height: 60vh; background: var(--bg-deep); border-radius: .5rem; }
    @media (max-width: 640px) {
      main { width: 100%; max-width: 100%; padding: .75rem; }
      header { display: grid; }
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
    <header>
      <div>
        <nav class="nav" aria-label="Label editor navigation">
          <a class="btn btn--secondary btn--pill" href="/player?show={{.Show}}">← Back to player</a>
          <a class="btn btn--secondary btn--pill" href="/">← Back to library</a>
        </nav>
        <h1 id="page-title">Labels for {{.Show}}</h1>
      </div>
      <form class="header-actions" method="post" action="/logout">
        <button type="submit" class="secondary">Sign out</button>
      </form>
    </header>
    <p class="status-line">
      <span id="save-state" class="save-state saved">Saved</span>
      <span class="status" id="status" role="status" aria-live="polite"></span>
      <span id="status-actions"></span>
    </p>
    <div class="actions" aria-label="Label editor actions">
      <button id="add-boundary" class="secondary mutating-control">Add boundary</button>
      <button id="save" class="primary mutating-control">Save</button>
    </div>
    <details class="help" id="shortcuts-help">
      <summary>Keyboard shortcuts</summary>
      <ul>
        <li><kbd>j</kbd> / <kbd>k</kbd> — Next / previous candidate (jumps the preview)</li>
        <li><kbd>Enter</kbd> — Promote the current candidate (prompts for a name)</li>
        <li><kbd>x</kbd> — Reject the current candidate</li>
        <li><kbd>r</kbd> — Replay from the current candidate's start time</li>
        <li><kbd>Alt</kbd> + <kbd>↑</kbd> / <kbd>↓</kbd> — Nudge the current candidate by ±5 s (the promoted boundary uses the new time)</li>
        <li><kbd>s</kbd> — Save</li>
        <li><kbd>d</kbd> — Scan silence only (advanced)</li>
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
        <p class="help">Auto-detect possible boundaries, then promote useful suggestions into named boundaries or reject noise.</p>
        <div class="autodetect-panel" aria-label="Auto-detect setup">
          <label class="field-label" for="autodetect-lineup">Lineup for auto-detection</label>
          <p class="help" id="autodetect-lineup-help">Enter one quartet name per non-empty line. Auto-detection uses the lineup to prefill candidate names, but every result still needs review.</p>
          <textarea id="autodetect-lineup" name="autodetect-lineup" class="editable-control" autocomplete="off" data-lpignore="true" aria-describedby="autodetect-lineup-help" placeholder="Quartet A&#10;Quartet B"></textarea>
          <div class="autodetect-options" aria-label="Auto-detect signal sources">
            <label><input type="checkbox" id="autodetect-use-silence" name="autodetect-use-silence" class="mutating-control" data-lpignore="true" checked> Use silence</label>
            <label><input type="checkbox" id="autodetect-use-color" name="autodetect-use-color" class="mutating-control" data-lpignore="true" checked> Use color</label>
            <label><input type="checkbox" id="autodetect-use-ocr" name="autodetect-use-ocr" class="mutating-control" data-lpignore="true"> Use OCR (slow)</label>
          </div>
          <button id="autodetect" class="primary mutating-control" aria-describedby="autodetect-lineup-help">Auto-detect boundaries</button>
        </div>
        <div class="candidate-tools" aria-label="Candidate filters">
          <label for="candidate-sort">Sort
            <select id="candidate-sort" name="candidate-sort" autocomplete="off" data-lpignore="true">
              <option value="duration-desc">Duration, longest first</option>
              <option value="review-priority">Review priority</option>
              <option value="time-asc">Time, earliest first</option>
            </select>
          </label>
          <label><input type="checkbox" id="hide-handled" name="hide-handled" data-lpignore="true" checked> Hide promoted/rejected</label>
          <label for="min-duration">Hide shorter than
            <input id="min-duration" name="min-duration" type="number" min="0" step="0.1" value="0" inputmode="decimal" autocomplete="off" data-lpignore="true"> seconds
          </label>
        </div>
        <div class="bulk-actions" aria-label="Bulk candidate actions">
          <label for="bulk-name">Bulk boundary name
            <input id="bulk-name" name="bulk-name" type="text" value="group-a" autocomplete="off" data-lpignore="true">
          </label>
          <button id="bulk-promote" class="secondary mutating-control" disabled>Promote selected</button>
          <button id="bulk-reject" class="danger mutating-control" disabled>Reject selected</button>
          <button id="promote-high-confidence" class="secondary mutating-control" disabled>Promote high-confidence</button>
        </div>
        <p class="help" id="candidate-count"></p>
        <div class="table-wrap candidate-table-wrap">
          <table class="candidate-table">
            <thead><tr><th><input type="checkbox" id="select-all-candidates" name="select-all-candidates" aria-label="Select all visible pending candidates" data-lpignore="true"></th><th>Time</th><th>Duration</th><th>Sources</th><th>Confidence</th><th>Status</th><th>Actions</th></tr></thead>
            <tbody id="candidates"></tbody>
          </table>
        </div>
      </section>
    </div>
    <details class="help advanced-tools" id="advanced-tools">
      <summary>Advanced tools</summary>
      <p class="help">Run a lower-level silence-only scan or exchange boundaries as plain-text timestamps.</p>
      <div class="actions" aria-label="Advanced label tools">
        <button id="detect" class="secondary mutating-control">Scan silence only</button>
        <button id="export" class="secondary mutating-control">Export timestamps</button>
        <button id="import" class="secondary mutating-control">Import timestamps</button>
      </div>
      <label class="field-label" for="timestamps">Plain-text boundaries for Export and Import</label>
      <p class="help">Use this box to copy boundaries out or paste edited boundaries back in. The format starts with a video header, then one boundary name and time per line.</p>
      <textarea id="timestamps" name="timestamps" autocomplete="off" data-lpignore="true" placeholder="&gt; {{.Show}}.mkv&#10;group-a 00:02:05"></textarea>
    </details>
  </main>
  <script src="/static/hls.min.js"></script>
  <script>
    const show = {{ .Show }};
    const api = '/labels/api/' + encodeURIComponent(show);
    let labels = { video: show, boundaries: [], candidates: [] };
    let dirty = false;
    let busy = false;
    let busyOperation = '';
    let currentKey = null;
    let backgroundPollTimer = null;
    let backgroundOperation = '';
    const selectedCandidates = new Set();
    const statusEl = document.getElementById('status');
    const statusActions = document.getElementById('status-actions');
    const saveStateEl = document.getElementById('save-state');
    const detectButton = document.getElementById('detect');
    const autoDetectButton = document.getElementById('autodetect');
    const autodetectLineup = document.getElementById('autodetect-lineup');
    const autodetectUseSilence = document.getElementById('autodetect-use-silence');
    const autodetectUseColor = document.getElementById('autodetect-use-color');
    const autodetectUseOCR = document.getElementById('autodetect-use-ocr');
    const sortControl = document.getElementById('candidate-sort');
    const hideHandledControl = document.getElementById('hide-handled');
    const minDurationControl = document.getElementById('min-duration');
    const selectAllCandidates = document.getElementById('select-all-candidates');
    const bulkName = document.getElementById('bulk-name');
    const bulkPromote = document.getElementById('bulk-promote');
    const bulkReject = document.getElementById('bulk-reject');
    const highConfidencePromote = document.getElementById('promote-high-confidence');
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
    const seekPreview = (seconds, opts) => {
      opts = opts || {};
      preview.currentTime = Math.max(0, Number(seconds) || 0);
      preview.play().catch(() => {});
      if (opts.scroll) preview.scrollIntoView({ behavior: 'smooth', block: 'start' });
    };
    const scrollRowIntoView = (row) => {
      if (!row) return;
      const wrap = row.closest('.candidate-table-wrap');
      if (!wrap) return;
      const header = wrap.querySelector('thead');
      const headerH = header ? header.getBoundingClientRect().height : 0;
      const rowRect = row.getBoundingClientRect();
      const wrapRect = wrap.getBoundingClientRect();
      const visibleTop = wrapRect.top + headerH;
      const visibleBottom = wrapRect.bottom;
      if (rowRect.top < visibleTop) {
        wrap.scrollTop -= (visibleTop - rowRect.top);
      } else if (rowRect.bottom > visibleBottom) {
        wrap.scrollTop += (rowRect.bottom - visibleBottom);
      }
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
    const candidateSuggestedName = (candidate) => String((candidate && candidate.suggestedName) || '').trim();
    const candidateDefaultName = (candidate, fallback) => candidateSuggestedName(candidate) || String(fallback || '').trim() || 'group-a';
    const isHandledCandidate = (candidate) => {
      const status = candidateStatus(candidate);
      return status === 'named' || status === 'rejected';
    };
    const candidateSources = (candidate) => Array.isArray(candidate.sources) ? candidate.sources.map(source => String(source || '').trim()).filter(Boolean) : [];
    const sourceDisplayName = (source) => ({
      silence: 'Silence',
      lineup: 'Lineup',
      scene: 'Scene',
      color: 'Color',
      black: 'Black',
      freeze: 'Freeze',
      ocr: 'OCR',
    }[String(source || '').toLowerCase()] || displayName(source));
    const sourceBadgeClass = (source) => {
      const normalized = String(source || '').toLowerCase();
      if (['silence', 'lineup', 'scene', 'color', 'black', 'freeze', 'ocr'].includes(normalized)) {
        return 'source-badge source-badge--' + normalized;
      }
      return 'source-badge';
    };
    const renderSourceBadges = (candidate) => {
      const sources = candidateSources(candidate);
      if (sources.length === 0) return '<span class="help">—</span>';
      return '<span class="source-badges">' + sources.map(source => '<span class="' + sourceBadgeClass(source) + '">' + escapeText(sourceDisplayName(source)) + '</span>').join('') + '</span>';
    };
    const formatConfidence = (value) => {
      const confidence = Number(value);
      if (!Number.isFinite(confidence) || confidence <= 0) return '—';
      return Math.round(confidence * 100) + '%';
    };
    const candidateLowConfidence = (candidate) => {
      const confidence = Number(candidate && candidate.confidence);
      return Number.isFinite(confidence) && confidence > 0 && confidence < 0.75;
    };
    const renderCandidateStatus = (candidate) => {
      const status = escapeText(candidateStatus(candidate));
      const badges = [];
      if (candidate.conflict) badges.push('<span class="conflict-badge">Conflict</span>');
      if (candidateLowConfidence(candidate)) badges.push('<span class="low-confidence-badge">Low confidence</span>');
      if (badges.length === 0) return status;
      return status + ' ' + badges.join(' ');
    };
    const candidateKey = (candidate) => String(Number(candidate.time) || 0) + '|' + String(Number(candidate.duration) || 0);
    const candidateReviewPriority = (candidate) => {
      let priority = 0;
      if (candidate.conflict) priority += 100;
      if (candidateLowConfidence(candidate)) priority += 50;
      if (candidateSources(candidate).length > 1) priority += 10;
      if (candidateSuggestedName(candidate)) priority += 5;
      return priority;
    };
    const candidateItems = () => {
      normalizeLabels();
      const minDuration = Math.max(0, Number(minDurationControl.value) || 0);
      const items = labels.candidates.map((candidate, index) => ({ candidate: candidate, index: index, key: candidateKey(candidate) })).filter((item) => {
        if (hideHandledControl.checked && isHandledCandidate(item.candidate)) return false;
        if ((Number(item.candidate.duration) || 0) < minDuration) return false;
        return true;
      });
      if (sortControl.value === 'review-priority') {
        items.sort((a, b) => candidateReviewPriority(b.candidate) - candidateReviewPriority(a.candidate) || a.index - b.index);
      } else if (sortControl.value === 'duration-desc') {
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
    const highConfidenceCandidateItems = () => {
      normalizeLabels();
      return labels.candidates.map((candidate, index) => ({ candidate: candidate, index: index, key: candidateKey(candidate) })).filter((item) => !isHandledCandidate(item.candidate) && !item.candidate.conflict && Number(item.candidate.confidence) >= 0.85 && candidateSuggestedName(item.candidate));
    };
    const updateBulkControls = () => {
      const selected = selectedVisiblePendingItems();
      bulkPromote.disabled = busy || selected.length === 0;
      bulkReject.disabled = busy || selected.length === 0;
      const highConfidence = highConfidenceCandidateItems();
      highConfidencePromote.disabled = busy || highConfidence.length === 0;
      const visiblePending = candidateItems().filter((item) => !isHandledCandidate(item.candidate));
      selectAllCandidates.disabled = busy || visiblePending.length === 0;
      selectAllCandidates.checked = visiblePending.length > 0 && visiblePending.every((item) => selectedCandidates.has(item.key));
      selectAllCandidates.indeterminate = selected.length > 0 && !selectAllCandidates.checked;
    };
    const setBusy = (value, operation) => {
      busy = Boolean(value);
      if (busy && operation) busyOperation = operation;
      if (!busy) busyOperation = '';
      document.querySelectorAll('.mutating-control, .editable-control').forEach((control) => { control.disabled = busy; });
      detectButton.textContent = busy && busyOperation === 'detect' ? 'Scanning…' : 'Scan silence only';
      autoDetectButton.textContent = busy && busyOperation === 'autodetect' ? 'Auto-detecting…' : 'Auto-detect boundaries';
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
    const visiblePendingItems = () => candidateItems().filter((item) => !isHandledCandidate(item.candidate));
    const currentCandidateItem = () => visiblePendingItems().find((item) => item.key === currentKey) || null;
    const clampCurrent = () => {
      const items = visiblePendingItems();
      if (!items.some((item) => item.key === currentKey)) {
        currentKey = items.length ? items[0].key : null;
      }
    };
    const ensureCurrent = () => {
      let item = currentCandidateItem();
      if (!item) {
        const items = visiblePendingItems();
        item = items.length ? items[0] : null;
        currentKey = item ? item.key : null;
      }
      return item;
    };
    const setCurrent = (key, opts) => {
      opts = opts || {};
      currentKey = key;
      render();
      if (opts.seek) {
        const item = currentCandidateItem();
        if (item) seekPreview(item.candidate.time);
      }
    };
    const stepCandidate = (direction) => {
      const items = visiblePendingItems();
      if (items.length === 0) { setStatus('No pending candidates to review. Run auto-detect first.'); return; }
      let idx = items.findIndex((item) => item.key === currentKey);
      if (idx === -1) idx = direction > 0 ? -1 : items.length;
      const nextIdx = idx + direction;
      if (nextIdx < 0) { setCurrent(items[0].key, { seek: true }); setStatus('Already at the first candidate.'); return; }
      if (nextIdx >= items.length) { setCurrent(items[items.length - 1].key, { seek: true }); setStatus('Already at the last candidate.'); return; }
      const next = items[nextIdx];
      setCurrent(next.key, { seek: true });
      setStatus('Candidate ' + (nextIdx + 1) + ' of ' + items.length + ' at ' + secondsToClock(next.candidate.time) + '.');
    };
    const nextPendingKeyAfter = (beforeItems, handledIndex) => {
      const stillPending = new Set(visiblePendingItems().map((item) => item.key));
      for (let i = handledIndex + 1; i < beforeItems.length; i++) { if (stillPending.has(beforeItems[i].key)) return beforeItems[i].key; }
      for (let i = handledIndex - 1; i >= 0; i--) { if (stillPending.has(beforeItems[i].key)) return beforeItems[i].key; }
      const remaining = visiblePendingItems();
      return remaining.length ? remaining[0].key : null;
    };
    const promoteCurrentCandidate = () => {
      const items = visiblePendingItems();
      if (items.length === 0) { setStatus('No pending candidates to promote.'); return; }
      const item = currentCandidateItem() || items[0];
      const handledIndex = items.findIndex((i) => i.key === item.key);
      const defaultName = candidateDefaultName(item.candidate, bulkName.value);
      const name = window.prompt('Boundary name for candidate at ' + secondsToClock(item.candidate.time), defaultName);
      if (name === null) { setStatus('Promote cancelled.'); return; }
      if (!promoteCandidate(item.candidate, name)) return;
      const time = item.candidate.time;
      const nextKey = nextPendingKeyAfter(items, handledIndex);
      setCurrent(nextKey, { seek: Boolean(nextKey) });
      setStatus('Promoted candidate at ' + secondsToClock(time) + '. Save to persist.');
    };
    const rejectCurrentCandidate = () => {
      const items = visiblePendingItems();
      if (items.length === 0) { setStatus('No pending candidates to reject.'); return; }
      const item = currentCandidateItem() || items[0];
      const handledIndex = items.findIndex((i) => i.key === item.key);
      const time = item.candidate.time;
      rejectCandidate(item.candidate);
      const nextKey = nextPendingKeyAfter(items, handledIndex);
      setCurrent(nextKey, { seek: Boolean(nextKey) });
      setStatus('Rejected candidate at ' + secondsToClock(time) + '. Save to persist.');
    };
    const replayCurrent = () => {
      const item = ensureCurrent();
      if (!item) { setStatus('No candidate to replay. Run auto-detect first.'); return; }
      render();
      seekPreview(item.candidate.time);
      setStatus('Replaying from ' + secondsToClock(item.candidate.time) + '.');
    };
    const nudgeCurrentCandidate = (delta) => {
      const item = ensureCurrent();
      if (!item) { setStatus('No candidate to nudge. Run auto-detect first.'); return; }
      const oldKey = candidateKey(item.candidate);
      const next = Math.max(0, (Number(item.candidate.time) || 0) + delta);
      item.candidate.time = next;
      const newKey = candidateKey(item.candidate);
      if (selectedCandidates.has(oldKey)) { selectedCandidates.delete(oldKey); selectedCandidates.add(newKey); }
      currentKey = newKey;
      setDirty(true);
      render();
      seekPreview(next);
      setStatus('Nudged candidate ' + (delta > 0 ? '+5s' : '-5s') + ' to ' + secondsToClock(next) + '. Enter to promote.');
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
        row.innerHTML = '<td><input type="text" class="editable-control" name="boundary-name-' + index + '" autocomplete="off" data-lpignore="true" value="' + escapeAttr(boundary.name || '') + '" aria-label="Boundary name"></td><td><input type="text" class="editable-control" name="boundary-time-' + index + '" autocomplete="off" data-lpignore="true" value="' + secondsToClock(boundary.start) + '" aria-label="Boundary start"></td><td><div class="row-actions"><button class="secondary preview">Preview</button><button class="danger delete mutating-control">Delete</button></div></td>';
        const inputs = row.querySelectorAll('input');
        inputs[0].addEventListener('input', () => { boundary.name = inputs[0].value; setDirty(true); });
        inputs[1].addEventListener('input', () => { try { boundary.start = clockToSeconds(inputs[1].value); setDirty(true); setStatus(''); } catch (err) { setStatus(err.message); } });
        row.querySelector('.preview').addEventListener('click', () => seekPreview(boundary.start, { scroll: true }));
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
      clampCurrent();
      const visibleCandidates = candidateItems();
      if (visibleCandidates.length === 0) {
        const row = document.createElement('tr');
        const message = (labels.candidates || []).length === 0 ? 'No candidates yet. Set up auto-detect above, or use Scan silence only under Advanced tools.' : 'No candidates match the current filters. Adjust the filters or run auto-detect again.';
        row.innerHTML = '<td colspan="7" class="empty-state">' + escapeText(message) + ' <button type="button" class="secondary empty-autodetect">Set up auto-detect</button></td>';
        row.querySelector('.empty-autodetect').addEventListener('click', () => {
          autodetectLineup.scrollIntoView({ behavior: 'smooth', block: 'center' });
          autodetectLineup.focus();
        });
        candidates.appendChild(row);
      } else {
        visibleCandidates.forEach((item) => {
          const candidate = item.candidate;
          const row = document.createElement('tr');
          if (item.key === currentKey) { row.classList.add('candidate-current'); row.setAttribute('aria-current', 'true'); }
          if (candidate.conflict) { row.classList.add('candidate-conflict'); }
          if (candidateLowConfidence(candidate)) { row.classList.add('candidate-low-confidence'); }
          const checked = selectedCandidates.has(item.key) ? ' checked' : '';
          const handled = isHandledCandidate(candidate);
          const disabled = handled ? ' disabled' : '';
          const inlineDefaultName = candidateSuggestedName(candidate) || 'group-a';
          const pendingActions = handled ? '<span class="help">Handled</span>' : '<label><span class="sr-only">Boundary name for candidate at ' + secondsToClock(candidate.time) + '</span><input type="text" class="inline-name editable-control" name="candidate-boundary-name-' + item.index + '" autocomplete="off" data-lpignore="true" value="' + escapeAttr(inlineDefaultName) + '" aria-label="Boundary name for candidate at ' + secondsToClock(candidate.time) + '"></label><button class="promote mutating-control">Promote</button><button class="danger reject mutating-control">Reject</button>';
          row.innerHTML = '<td><input type="checkbox" class="candidate-select" name="candidate-select-' + item.index + '" aria-label="Select candidate at ' + secondsToClock(candidate.time) + '" data-lpignore="true"' + checked + disabled + '></td><td>' + secondsToClock(candidate.time) + '</td><td>' + Number(candidate.duration || 0).toFixed(2) + 's</td><td>' + renderSourceBadges(candidate) + '</td><td>' + escapeText(formatConfidence(candidate.confidence)) + '</td><td>' + renderCandidateStatus(candidate) + '</td><td><div class="row-actions"><button class="secondary preview">Preview</button>' + pendingActions + '</div></td>';
          const checkbox = row.querySelector('.candidate-select');
          checkbox.addEventListener('change', () => {
            if (checkbox.checked) selectedCandidates.add(item.key);
            else selectedCandidates.delete(item.key);
            updateBulkControls();
          });
          row.querySelector('.preview').addEventListener('click', () => seekPreview(candidate.time, { scroll: true }));
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
      const currentRow = candidates.querySelector('tr.candidate-current');
      scrollRowIntoView(currentRow);
      setBusy(busy);
    };
    const loadLabels = async () => {
      const res = await fetch(api);
      if (!res.ok) throw new Error(await res.text());
      labels = await res.json();
      normalizeLabels();
      selectedCandidates.clear();
      render();
      setDirty(false);
    };
    const clearBackgroundPoll = () => {
      if (backgroundPollTimer !== null) {
        window.clearTimeout(backgroundPollTimer);
        backgroundPollTimer = null;
      }
    };
    const scheduleBackgroundPoll = (operation) => {
      clearBackgroundPoll();
      backgroundPollTimer = window.setTimeout(() => checkBackgroundStatus(operation), 3000);
    };
    const applyBackgroundStatus = async (operation, job) => {
      const state = job && job.state ? job.state : 'idle';
      if (state === 'running') {
        backgroundOperation = operation;
        setBusy(true, operation);
        const message = operation === 'autodetect'
          ? 'Auto-detecting boundaries in the background… You can close this page; results will be saved for review.'
          : 'Scanning silence in the background… You can close this page; results will be saved for review.';
        setStatus(message);
        scheduleBackgroundPoll(operation);
        return;
      }

      if (backgroundOperation === operation) {
        backgroundOperation = '';
        clearBackgroundPoll();
        setBusy(false);
      }
      if (state === 'completed') {
        await loadLabels();
        const count = Number(job.candidateCount) || 0;
        const verb = operation === 'autodetect' ? 'Auto-detected' : 'Found';
        setStatus(verb + ' ' + count + ' pending candidate boundary(ies) and saved them for review.');
      } else if (state === 'failed') {
        const prefix = operation === 'autodetect' ? 'Auto-detect failed: ' : 'Silence scan failed: ';
        setStatus(prefix + (job.error || 'unknown error'));
      }
    };
    const fetchBackgroundStatus = async (operation) => {
      const res = await fetch(api + '/' + operation);
      if (!res.ok) throw new Error(await res.text());
      return await res.json();
    };
    const checkBackgroundStatus = async (operation) => {
      try {
        await applyBackgroundStatus(operation, await fetchBackgroundStatus(operation));
      } catch (err) {
        if (backgroundOperation === operation) {
          setStatus('Background analysis is still running, but its status is temporarily unavailable. Retrying…');
          scheduleBackgroundPoll(operation);
        } else {
          setBusy(false);
          setStatus('Could not check background analysis status: ' + err.message);
        }
      }
    };
    const resumeBackgroundJobs = async () => {
      const operations = ['autodetect', 'detect'];
      const statuses = await Promise.all(operations.map(async (operation) => {
        try {
          return { operation: operation, job: await fetchBackgroundStatus(operation) };
        } catch (_) {
          return null;
        }
      }));
      const available = statuses.filter(Boolean);
      const running = available.find((item) => item.job && item.job.state === 'running');
      if (running) {
        await applyBackgroundStatus(running.operation, running.job);
        return;
      }
      const finished = available.filter((item) => item.job && (item.job.state === 'completed' || item.job.state === 'failed')).sort((a, b) => String(b.job.finishedAt || '').localeCompare(String(a.job.finishedAt || '')));
      if (finished.length > 0) {
        await applyBackgroundStatus(finished[0].operation, finished[0].job);
      }
    };
    const runDetect = async () => {
      if (busy) return;
      if (dirty) {
        setStatus('Save your current label changes before starting detection.');
        return;
      }
      backgroundOperation = 'detect';
      setBusy(true, 'detect');
      setStatus('Starting background silence-only scan…');
      try {
        const res = await fetch(api + '/detect', { method: 'POST' });
        if (!res.ok) throw new Error(await res.text());
        await applyBackgroundStatus('detect', await res.json());
      } catch (err) {
        backgroundOperation = '';
        setBusy(false);
        setStatus('Silence scan failed: ' + err.message);
      }
    };
    const parseAutodetectLineup = () => autodetectLineup.value.split(/\r?\n/).map((name) => name.trim()).filter(Boolean).map((name) => ({ name: name }));
    const runAutodetect = async () => {
      if (busy) return;
      if (dirty) {
        setStatus('Save your current label changes before starting auto-detection.');
        return;
      }
      const lineup = parseAutodetectLineup();
      if (lineup.length === 0) {
        setStatus('Enter at least one quartet name before starting auto-detection.');
        autodetectLineup.focus();
        return;
      }
      backgroundOperation = 'autodetect';
      setBusy(true, 'autodetect');
      setStatus('Starting background auto-detection…');
      try {
        const res = await fetch(api + '/autodetect', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            lineup: lineup,
            useSilence: autodetectUseSilence.checked,
            useColor: autodetectUseColor.checked,
            useOCR: autodetectUseOCR.checked,
          }),
        });
        if (!res.ok) throw new Error(await res.text());
        await applyBackgroundStatus('autodetect', await res.json());
      } catch (err) {
        backgroundOperation = '';
        setBusy(false);
        setStatus('Auto-detect failed: ' + err.message);
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
    autoDetectButton.addEventListener('click', runAutodetect);
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
    highConfidencePromote.addEventListener('click', () => {
      const highConfidence = highConfidenceCandidateItems();
      if (highConfidence.length === 0) {
        setStatus('No high-confidence suggestions to promote.');
        return;
      }
      highConfidence.forEach((item) => promoteCandidate(item.candidate, candidateSuggestedName(item.candidate)));
      render();
      setStatus('Promoted ' + highConfidence.length + ' high-confidence candidate(s). Save to persist.');
    });
    bulkReject.addEventListener('click', () => {
      const selected = selectedVisiblePendingItems();
      selected.forEach((item) => rejectCandidate(item.candidate));
      render();
      setStatus('Rejected ' + selected.length + ' selected candidate(s). Save to persist.');
    });
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
      if (isTypingTarget(document.activeElement)) return;
      if (event.altKey && !event.ctrlKey && !event.metaKey && (event.key === 'ArrowUp' || event.key === 'ArrowDown')) {
        event.preventDefault();
        nudgeCurrentCandidate(event.key === 'ArrowUp' ? 5 : -5);
        return;
      }
      if (event.ctrlKey || event.altKey || event.metaKey) return;
      const active = document.activeElement;
      const activeIsButtonlike = active && (active.tagName === 'BUTTON' || active.tagName === 'A');
      const key = event.key.length === 1 ? event.key.toLowerCase() : event.key;
      let handled = true;
      switch (key) {
        case 's': document.getElementById('save').click(); break;
        case 'd': runDetect(); break;
        case 'j': stepCandidate(1); break;
        case 'k': stepCandidate(-1); break;
        case 'x': rejectCurrentCandidate(); break;
        case 'r': replayCurrent(); break;
        case 'Enter': if (activeIsButtonlike) { handled = false; } else { promoteCurrentCandidate(); } break;
        case '?': openShortcutsHelp(); break;
        default: handled = false;
      }
      if (handled) event.preventDefault();
    });
    (async () => {
      try {
        await loadLabels();
      } catch (err) {
        setStatus(err.message);
        return;
      }
      await resumeBackgroundJobs();
    })();
  </script>
</body>
</html>`))

// RegisterRoutes wires the label UI and JSON API endpoints into mux.
func (s *Store) RegisterRoutes(mux *http.ServeMux, a *auth.Authenticator) {
	NewServer(s.cfg, s).RegisterRoutes(mux, a)
}

// RegisterRoutes wires the label UI and JSON API endpoints into mux.
func (srv *Server) RegisterRoutes(mux *http.ServeMux, a *auth.Authenticator) {
	mux.Handle("GET /labels/{show}", a.RequirePage(http.HandlerFunc(srv.handleLabelsPage)))
	mux.Handle("GET /labels/api/{show}", a.RequireMedia(http.HandlerFunc(srv.handleLabelsGet)))
	mux.Handle("POST /labels/api/{show}", a.RequireMedia(http.HandlerFunc(srv.handleLabelsPost)))
	mux.Handle("POST /labels/api/{show}/import", a.RequireMedia(http.HandlerFunc(srv.handleLabelsImport)))
	mux.Handle("GET /labels/api/{show}/export", a.RequireMedia(http.HandlerFunc(srv.handleLabelsExport)))
	mux.Handle("POST /labels/api/{show}/mkv/import", a.RequireMedia(http.HandlerFunc(srv.handleMKVImport)))
	mux.Handle("POST /labels/api/{show}/mkv/embed", a.RequireMedia(http.HandlerFunc(srv.handleMKVEmbed)))
	mux.Handle("GET /labels/api/{show}/detect", a.RequireMedia(http.HandlerFunc(srv.handleDetectStatus)))
	mux.Handle("POST /labels/api/{show}/detect", a.RequireMedia(http.HandlerFunc(srv.handleDetect)))
	mux.Handle("GET /labels/api/{show}/autodetect", a.RequireMedia(http.HandlerFunc(srv.handleAutodetectStatus)))
	mux.Handle("POST /labels/api/{show}/autodetect", a.RequireMedia(http.HandlerFunc(srv.handleAutodetect)))
}

func (srv *Server) handleLabelsPage(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := labelsPageTemplate.Execute(w, struct{ Show string }{Show: show}); err != nil {
		http.Error(w, "failed to render labels page", http.StatusInternalServerError)
	}
}

func (srv *Server) handleLabelsGet(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	labels, err := srv.store.Load(show)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, labels)
}

func (srv *Server) handleLabelsPost(w http.ResponseWriter, r *http.Request) {
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
	srv.mutationMu.Lock()
	defer srv.mutationMu.Unlock()
	if err := srv.saveAndWriteChapters(labels); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (srv *Server) handleLabelsImport(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	defer r.Body.Close()
	labels, err := importTimestamps(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	labels.Video = show
	if err := validateBoundaryNames(labels.Boundaries); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	srv.mutationMu.Lock()
	defer srv.mutationMu.Unlock()
	if err := srv.saveAndWriteChapters(labels); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

func (srv *Server) handleLabelsExport(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	labels, err := srv.store.Load(show)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, exportTimestamps(labels))
}

func (srv *Server) handleMKVImport(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	srv.mutationMu.Lock()
	defer srv.mutationMu.Unlock()

	labels, err := srv.store.Load(show)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	boundaries, err := importMKVChapters(filepath.Join(srv.cfg.VideoDir, show+".mkv"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	labels.Boundaries = sortedBoundaries(append(labels.Boundaries, boundaries...))
	labels.Video = show
	if err := srv.saveAndWriteChapters(labels); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, labels)
}

func (srv *Server) handleMKVEmbed(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	labels, err := srv.store.Load(show)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := exportMKVChapters(filepath.Join(srv.cfg.VideoDir, show+".mkv"), labels.Boundaries); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (srv *Server) handleDetectStatus(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, srv.detections.status(show, detectionOperationSilence))
}

func (srv *Server) handleAutodetectStatus(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, srv.detections.status(show, detectionOperationAutodetect))
}

// handleDetect starts server-owned silence detection and returns immediately.
// The background job persists candidates when it completes, independent of the
// browser request that started it.
func (srv *Server) handleDetect(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted, srv.detections.startSilence(show))
}

// handleAutodetect validates the requested signals and starts a server-owned
// background job whose candidates are persisted independently of the page.
func (srv *Server) handleAutodetect(w http.ResponseWriter, r *http.Request) {
	show, ok := validShowFromRequest(w, r)
	if !ok {
		return
	}
	defer r.Body.Close()
	req, err := decodeAutodetectRequest(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusAccepted, srv.detections.startAutodetect(show, req))
}

// mergeCandidates keeps candidates the user has already decided on
// (status "named" or "rejected") and adds freshly detected candidates whose
// time is not within one second of a kept one. The result is sorted by time.
func mergeCandidates(existing, detected []Candidate) []Candidate {
	return mergeCandidatesWithBoundaries(existing, detected, nil)
}

func mergeCandidatesWithBoundaries(existing, detected []Candidate, boundaries []Boundary) []Candidate {
	var kept []Candidate
	for _, c := range existing {
		if c.Status == "named" || c.Status == "rejected" {
			kept = append(kept, c)
		}
	}
	result := append([]Candidate(nil), kept...)
	for _, d := range detected {
		if nearAnyCandidate(d.Time, kept) || nearAnyBoundary(d.Time, boundaries) {
			continue
		}
		if mergeIntoNearbyDetected(result, d, len(kept)) {
			continue
		}
		result = append(result, d)
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].Time < result[j].Time })
	return result
}

func nearAnyCandidate(t float64, candidates []Candidate) bool {
	for _, c := range candidates {
		if math.Abs(c.Time-t) < 1.0 {
			return true
		}
	}
	return false
}

func nearAnyBoundary(t float64, boundaries []Boundary) bool {
	for _, b := range boundaries {
		if math.Abs(b.Start-t) < 1.0 {
			return true
		}
	}
	return false
}

func mergeIntoNearbyDetected(result []Candidate, detected Candidate, start int) bool {
	if !hasCandidateMetadata(detected) {
		return false
	}
	for i := start; i < len(result); i++ {
		if math.Abs(result[i].Time-detected.Time) >= 1.0 || !hasCandidateMetadata(result[i]) {
			continue
		}
		result[i] = mergeCandidateMetadata(result[i], detected)
		return true
	}
	return false
}

func hasCandidateMetadata(c Candidate) bool {
	return len(c.Sources) > 0 || c.Confidence != 0 || c.SuggestedName != "" || c.Conflict
}

func mergeCandidateMetadata(a, b Candidate) Candidate {
	merged := a
	merged.Sources = unionSources(a.Sources, b.Sources)
	if b.Confidence > merged.Confidence {
		merged.Confidence = b.Confidence
	}
	if merged.SuggestedName == "" {
		merged.SuggestedName = b.SuggestedName
	}
	merged.Conflict = merged.Conflict || b.Conflict
	return merged
}

func unionSources(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, source := range append(append([]string(nil), a...), b...) {
		if source == "" {
			continue
		}
		if _, ok := seen[source]; ok {
			continue
		}
		seen[source] = struct{}{}
		out = append(out, source)
	}
	return out
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

func (srv *Server) saveAndWriteChapters(labels VideoLabels) error {
	if err := srv.store.Save(labels); err != nil {
		return err
	}
	dir := filepath.Join(srv.cfg.HLSDir, labels.Video)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "chapters.vtt"), []byte(toWebVTT(labels)), 0o644)
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
