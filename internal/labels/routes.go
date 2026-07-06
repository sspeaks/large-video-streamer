package labels

import (
	"encoding/json"
	"errors"
	"html/template"
	"io"
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
  <style>
    :root { color-scheme: dark; font-family: system-ui, -apple-system, Segoe UI, sans-serif; }
    body { margin: 0; background: #0f172a; color: #e5e7eb; }
    main { width: min(1100px, calc(100vw - 2rem)); margin: 0 auto; padding: 1rem; }
    h1, h2 { margin: 1rem 0 .75rem; }
    section { margin: 1rem 0; padding: 1rem; border: 1px solid #334155; border-radius: .75rem; background: #111827; }
    table { width: 100%; border-collapse: collapse; }
    th, td { padding: .5rem; border-bottom: 1px solid #334155; text-align: left; }
    input, textarea { box-sizing: border-box; width: 100%; padding: .55rem; border: 1px solid #475569; border-radius: .4rem; background: #020617; color: #f8fafc; }
    textarea { min-height: 9rem; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
    button { margin: .15rem; padding: .5rem .7rem; border: 0; border-radius: .4rem; background: #38bdf8; color: #082f49; font-weight: 700; cursor: pointer; }
    button.danger { background: #f87171; color: #450a0a; }
    button.secondary { background: #64748b; color: #f8fafc; }
    .actions { display: flex; flex-wrap: wrap; gap: .4rem; margin: .75rem 0; }
    .status { min-height: 1.4rem; color: #93c5fd; }
    .grid { display: grid; gap: 1rem; grid-template-columns: repeat(auto-fit, minmax(22rem, 1fr)); }
  </style>
</head>
<body>
  <main>
    <h1>Labels for {{.Show}}</h1>
    <p class="status" id="status"></p>
    <div class="actions">
      <button id="add-boundary">Add boundary</button>
      <button id="save">Save</button>
      <button id="export">Export timestamps</button>
      <button id="import">Import timestamps</button>
    </div>
    <div class="grid">
      <section>
        <h2>Boundaries</h2>
        <table>
          <thead><tr><th>Name</th><th>Start (HH:MM:SS)</th><th>Actions</th></tr></thead>
          <tbody id="boundaries"></tbody>
        </table>
      </section>
      <section>
        <h2>Candidates</h2>
        <table>
          <thead><tr><th>Time</th><th>Duration</th><th>Status</th><th>Actions</th></tr></thead>
          <tbody id="candidates"></tbody>
        </table>
      </section>
    </div>
    <section>
      <h2>clipTrimmer timestamps</h2>
      <textarea id="timestamps" placeholder="group-a 00:01:00&#10;group-b 00:02:00"></textarea>
    </section>
  </main>
  <script>
    const show = {{printf "%q" .Show}};
    const api = '/labels/api/' + encodeURIComponent(show);
    let labels = { video: show, boundaries: [], candidates: [] };
    const statusEl = document.getElementById('status');
    const setStatus = (message) => { statusEl.textContent = message; };
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
    const render = () => {
      const boundaries = document.getElementById('boundaries');
      boundaries.innerHTML = '';
      (labels.boundaries || []).forEach((boundary, index) => {
        const row = document.createElement('tr');
        row.innerHTML = '<td><input value="' + escapeAttr(boundary.name || '') + '" aria-label="Boundary name"></td><td><input value="' + secondsToClock(boundary.start) + '" aria-label="Boundary start"></td><td><button class="danger">Delete</button></td>';
        const inputs = row.querySelectorAll('input');
        inputs[0].addEventListener('input', () => boundary.name = inputs[0].value);
        inputs[1].addEventListener('input', () => { try { boundary.start = clockToSeconds(inputs[1].value); setStatus(''); } catch (err) { setStatus(err.message); } });
        row.querySelector('button').addEventListener('click', () => { labels.boundaries.splice(index, 1); render(); });
        boundaries.appendChild(row);
      });
      const candidates = document.getElementById('candidates');
      candidates.innerHTML = '';
      (labels.candidates || []).forEach((candidate) => {
        const row = document.createElement('tr');
        row.innerHTML = '<td>' + secondsToClock(candidate.time) + '</td><td>' + Number(candidate.duration || 0).toFixed(2) + 's</td><td>' + escapeText(candidate.status || 'candidate') + '</td><td><button>Promote</button><button class="danger">Reject</button></td>';
        row.querySelector('button').addEventListener('click', () => {
          const name = prompt('Boundary name', 'group-a');
          if (name) {
            labels.boundaries.push({ name, start: Number(candidate.time) || 0 });
            candidate.status = 'named';
            render();
          }
        });
        row.querySelector('.danger').addEventListener('click', () => { candidate.status = 'rejected'; render(); });
        candidates.appendChild(row);
      });
    };
    const escapeText = (value) => String(value).replace(/[&<>"]/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;'}[c]));
    const escapeAttr = escapeText;
    document.getElementById('add-boundary').addEventListener('click', () => { labels.boundaries = labels.boundaries || []; labels.boundaries.push({ name: 'group-a', start: 0 }); render(); });
    document.getElementById('save').addEventListener('click', async () => {
      const res = await fetch(api, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(labels) });
      if (!res.ok) throw new Error(await res.text());
      setStatus('Saved labels and chapters.vtt');
    });
    document.getElementById('export').addEventListener('click', async () => {
      const res = await fetch(api + '/export');
      if (!res.ok) throw new Error(await res.text());
      document.getElementById('timestamps').value = await res.text();
      setStatus('Exported timestamps');
    });
    document.getElementById('import').addEventListener('click', async () => {
      const res = await fetch(api + '/import', { method: 'POST', body: document.getElementById('timestamps').value });
      if (!res.ok) throw new Error(await res.text());
      labels = await res.json();
      render();
      setStatus('Imported timestamps and wrote chapters.vtt');
    });
    (async () => {
      try {
        const res = await fetch(api);
        if (!res.ok) throw new Error(await res.text());
        labels = await res.json();
        labels.boundaries = labels.boundaries || [];
        labels.candidates = labels.candidates || [];
        render();
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
