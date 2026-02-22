package main

import (
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type buildTargetTiming struct {
	name     string
	project  string
	duration time.Duration
}

type targetTimingTracker struct {
	currentName    string
	currentProject string
	currentStart   time.Time
	rows           []buildTargetTiming
	spans          map[string]buildTargetTimingSpan
}

type buildTargetTimingSpan struct {
	name    string
	project string
	first   time.Time
	last    time.Time
}

func newTargetTimingTracker() *targetTimingTracker {
	return &targetTimingTracker{
		rows:  make([]buildTargetTiming, 0),
		spans: make(map[string]buildTargetTimingSpan),
	}
}

func (t *targetTimingTracker) processLine(line string, now time.Time) {
	target, project, ok := parseTargetStartLine(line)
	if ok {
		if t.currentName != "" && !t.currentStart.IsZero() {
			t.rows = append(t.rows, buildTargetTiming{
				name:     t.currentName,
				project:  t.currentProject,
				duration: now.Sub(t.currentStart),
			})
		}
		t.currentName = target
		t.currentProject = project
		t.currentStart = now
	}

	ctxTarget, ctxProject, ok := parseTargetContextLine(line)
	if ok {
		key := ctxProject + "::" + ctxTarget
		span, exists := t.spans[key]
		if !exists {
			span = buildTargetTimingSpan{
				name:    ctxTarget,
				project: ctxProject,
				first:   now,
				last:    now,
			}
		} else {
			span.last = now
		}
		t.spans[key] = span
	}
}

func (t *targetTimingTracker) finish(now time.Time) {
	if t.currentName != "" && !t.currentStart.IsZero() {
		t.rows = append(t.rows, buildTargetTiming{
			name:     t.currentName,
			project:  t.currentProject,
			duration: now.Sub(t.currentStart),
		})
	}
	t.currentName = ""
	t.currentProject = ""
	t.currentStart = time.Time{}
	t.mergeRowsFromSpans()
}

func parseTargetStartLine(line string) (target string, project string, ok bool) {
	match := targetStartRe.FindStringSubmatch(line)
	if len(match) == 3 {
		return strings.TrimSpace(match[1]), strings.TrimSpace(match[2]), true
	}
	return "", "", false
}

func parseTargetContextLine(line string) (target string, project string, ok bool) {
	match := targetContextRe.FindStringSubmatch(line)
	if len(match) == 3 {
		return strings.TrimSpace(match[1]), strings.TrimSpace(match[2]), true
	}
	return "", "", false
}

func (t *targetTimingTracker) mergeRowsFromSpans() {
	if len(t.spans) == 0 {
		return
	}
	seen := make(map[string]bool)
	for _, row := range t.rows {
		seen[row.project+"::"+row.name] = true
	}
	for key, span := range t.spans {
		if seen[key] {
			continue
		}
		duration := span.last.Sub(span.first)
		if duration < 0 {
			duration = 0
		}
		t.rows = append(t.rows, buildTargetTiming{
			name:     span.name,
			project:  span.project,
			duration: duration,
		})
	}
}

func dependencyTargetRows(cfg buildConfig, rows []buildTargetTiming) []buildTargetTiming {
	if len(rows) == 0 {
		return nil
	}
	rootProject := strings.TrimSuffix(filepath.Base(cfg.projectPath), filepath.Ext(cfg.projectPath))
	out := make([]buildTargetTiming, 0, len(rows))
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.name), strings.TrimSpace(cfg.scheme)) {
			continue
		}
		project := strings.TrimSpace(row.project)
		if project == "" {
			continue
		}
		if rootProject != "" && strings.EqualFold(project, rootProject) {
			continue
		}
		out = append(out, row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].duration > out[j].duration
	})
	return out
}

func completedFromTargetRows(rows []buildTargetTiming) []completedItem {
	if len(rows) == 0 {
		return nil
	}
	out := make([]completedItem, 0, len(rows))
	for _, row := range rows {
		out = append(out, completedItem{
			Name:       row.name,
			TaskCount:  0,
			DurationMS: row.duration.Milliseconds(),
		})
	}
	return out
}
