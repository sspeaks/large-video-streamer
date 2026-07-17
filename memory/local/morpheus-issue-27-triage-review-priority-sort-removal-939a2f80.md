---
id: 939a2f80-2d79-44e7-ab29-36da01d5e83f
class: LOCAL
loadGuidance: [ON-DEMAND]
title: "Issue #27 triage: review-priority sort removal"
author: "Morpheus"
createdAt: 2026-07-17T02:28:42.097Z
metadata: {}
---

## Issue #27 Triage Investigation

**Finding:** The issue title "The candidates order of appearence lsit doesnt work and isnt very helpful" (with typos) is ambiguous, but most likely refers to the "Review priority" sort option added in commit 42937bc.

**Current sort options in labeling UI (routes.go line 176-180):**
- "Duration, longest first" — sorts by duration descending
- "Review priority" — NEW: sorts by composite priority score (conflicts +100, low-confidence +50, multi-source +10, suggested-name +5)  
- "Time, earliest first" — sorts by time ascending (chronological/original order)

**Hypothesis:** The "review-priority" sort is the confusing feature. It was added to help prioritize which candidates to review first, but it may not be intuitive or valuable to users. Removing it simplifies the candidate-review UX from 3 sort options to 2 (duration vs. chronological order).

**Implementation owner:** Tank (Application Engineer) — this is a web-UX change, not a detection/labeling change. The change is isolated to `internal/web` (or `internal/labels/routes.go` which Tank owns for labeling page UX).

**Invariant to preserve:** Candidates must still be reviewable, promotable, rejectable, and filterable by duration. The "hide handled" and "hide shorter than" filters remain. Only the sort option is removed.

**No cross-domain contract change needed** — sort is purely UI-side, no API/data model impact.
