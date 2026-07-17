package labels

import (
	"math"
	"sort"
	"strings"
)

const (
	autodetectDuplicateSpacingSeconds  = 3.0
	autodetectLineupAssignmentMinScore = 1.0
	autodetectLineupConfidenceWeight   = 3.0
	autodetectLineupSkipPenalty        = 0.01
	autodetectLineupShortGapSeconds    = 6.0
	autodetectLineupShortGapPenalty    = 0.06
	autodetectLineupSourceBonus        = 0.15
	autodetectLineupNameMatchBonus     = 2.0
	autodetectLineupOCRMatchBonus      = 1.0
	autodetectLineupOCRConflictCost    = 6.0
	autodetectLineupStaleNameCost      = 3.0
	autodetectLineupNameMismatchCost   = 1.0
)

var (
	autodetectLineupOutputMinScore = 1.2
	autodetectSilenceOutputMinDur  = 6.0
)

func rankLineupSuggestions(lineup []autodetectLineupEntry, candidates []Candidate) []Candidate {
	sorted := append([]Candidate(nil), candidates...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return autodetectCandidateFusionTime(sorted[i]) < autodetectCandidateFusionTime(sorted[j])
	})
	sorted = pruneNearbyAutodetectDuplicates(sorted, autodetectDuplicateSpacingSeconds)

	names := lineupSuggestedNames(lineup)
	assignments := eligibleMonotoneLineupAssignments(names, sorted)
	hasCorroboratingSource := hasAutodetectCorroborationSource(sorted)
	filtered := make([]Candidate, 0, len(sorted))
	for i := range sorted {
		if lineupName, ok := assignments[i]; ok {
			filtered = append(filtered, assignLineupSuggestion(sorted[i], lineupName))
			continue
		}
		candidate := cleanupUnassignedLineupSuggestion(sorted[i])
		if dropUnassignedAutodetectCandidate(candidate, hasCorroboratingSource) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	return filtered
}

func eligibleMonotoneLineupAssignments(names []string, candidates []Candidate) map[int]string {
	eligibleIndexes := make([]int, 0, len(candidates))
	eligibleCandidates := make([]Candidate, 0, len(candidates))
	for i, candidate := range candidates {
		if !lineupAssignmentCandidateEligible(candidate, names) {
			continue
		}
		eligibleIndexes = append(eligibleIndexes, i)
		eligibleCandidates = append(eligibleCandidates, candidate)
	}

	eligibleAssignments := monotoneLineupAssignments(names, eligibleCandidates)
	assignments := make(map[int]string, len(eligibleAssignments))
	for eligibleIndex, lineupName := range eligibleAssignments {
		assignments[eligibleIndexes[eligibleIndex]] = lineupName
	}
	return assignments
}

func monotoneLineupAssignments(names []string, candidates []Candidate) map[int]string {
	slotCount := len(names)
	if slotCount > len(candidates) {
		slotCount = len(candidates)
	}
	assignments := make(map[int]string, slotCount)
	if slotCount == 0 {
		return assignments
	}

	scores := make([][]float64, slotCount)
	previous := make([][]int, slotCount)
	for slot := 0; slot < slotCount; slot++ {
		scores[slot] = make([]float64, len(candidates))
		previous[slot] = make([]int, len(candidates))
		for i := range scores[slot] {
			scores[slot][i] = math.Inf(-1)
			previous[slot][i] = -1
		}

		maxIndex := len(candidates) - (slotCount - slot)
		for candidateIndex := slot; candidateIndex <= maxIndex; candidateIndex++ {
			score := lineupAssignmentCandidateScore(candidates[candidateIndex], names[slot])
			if slot == 0 {
				scores[slot][candidateIndex] = score - autodetectLineupSkipPenalty*float64(candidateIndex)
				continue
			}

			for previousIndex := slot - 1; previousIndex < candidateIndex; previousIndex++ {
				if math.IsInf(scores[slot-1][previousIndex], -1) {
					continue
				}
				skipped := candidateIndex - previousIndex - 1
				pathScore := scores[slot-1][previousIndex] + score +
					lineupAssignmentTransitionScore(candidates[previousIndex], candidates[candidateIndex]) -
					autodetectLineupSkipPenalty*float64(skipped)
				if pathScore > scores[slot][candidateIndex] {
					scores[slot][candidateIndex] = pathScore
					previous[slot][candidateIndex] = previousIndex
				}
			}
		}
	}

	bestIndex := slotCount - 1
	bestScore := math.Inf(-1)
	for candidateIndex := slotCount - 1; candidateIndex < len(candidates); candidateIndex++ {
		score := scores[slotCount-1][candidateIndex]
		if score > bestScore {
			bestScore = score
			bestIndex = candidateIndex
		}
	}

	for slot, candidateIndex := slotCount-1, bestIndex; slot >= 0 && candidateIndex >= 0; slot-- {
		assignments[candidateIndex] = names[slot]
		candidateIndex = previous[slot][candidateIndex]
	}
	return assignments
}

func lineupAssignmentCandidateScore(candidate Candidate, lineupName string) float64 {
	score := lineupAssignmentEvidenceScore(candidate)

	suggestion := strings.TrimSpace(candidate.SuggestedName)
	if suggestion == "" {
		return score
	}
	if compatibleLineupSuggestion(suggestion, lineupName) {
		score += autodetectLineupNameMatchBonus
		if sourceContains(candidate.Sources, autodetectSourceOCR) {
			score += autodetectLineupOCRMatchBonus
		}
		return score
	}
	if sourceContains(candidate.Sources, autodetectSourceOCR) {
		return score - autodetectLineupOCRConflictCost
	}
	if sourceContains(candidate.Sources, autodetectSourceLineup) {
		return autodetectSilenceConfidence*autodetectLineupConfidenceWeight - autodetectLineupStaleNameCost
	}
	return score - autodetectLineupNameMismatchCost
}

func lineupAssignmentCandidateEligible(candidate Candidate, names []string) bool {
	for _, name := range names {
		if lineupAssignmentCandidateScore(candidate, name) >= autodetectLineupAssignmentMinScore {
			return true
		}
	}
	return false
}

func lineupAssignmentEvidenceScore(candidate Candidate) float64 {
	confidence := candidate.Confidence
	if confidence <= 0 {
		confidence = autodetectSilenceConfidence
	}
	if confidence > 1 {
		confidence = 1
	}
	return confidence*autodetectLineupConfidenceWeight + lineupAssignmentSourceBonus(candidate)
}

func lineupAssignmentSourceBonus(candidate Candidate) float64 {
	seen := make(map[string]struct{}, len(candidate.Sources))
	for _, source := range candidate.Sources {
		if source == "" || source == autodetectSourceLineup || source == autodetectSourceAudio {
			continue
		}
		seen[source] = struct{}{}
	}
	if len(seen) <= 1 {
		return 0
	}
	return math.Min(float64(len(seen)-1)*autodetectLineupSourceBonus, autodetectLineupSourceBonus*3)
}

func lineupAssignmentTransitionScore(previous Candidate, current Candidate) float64 {
	gap := autodetectCandidateFusionTime(current) - autodetectCandidateFusionTime(previous)
	if gap >= autodetectLineupShortGapSeconds {
		return 0
	}
	if gap < 0 {
		return -autodetectLineupShortGapSeconds * autodetectLineupShortGapPenalty
	}
	return -(autodetectLineupShortGapSeconds - gap) * autodetectLineupShortGapPenalty
}

func cleanupUnassignedLineupSuggestion(candidate Candidate) Candidate {
	if sourceContains(candidate.Sources, autodetectSourceLineup) && !sourceContains(candidate.Sources, autodetectSourceOCR) {
		candidate.SuggestedName = ""
		candidate.Sources = removeSource(candidate.Sources, autodetectSourceLineup)
	}
	if candidate.SuggestedName == "" && len(candidate.Sources) == 0 {
		candidate.Sources = []string{autodetectSourceSilence}
		candidate.Confidence = autodetectSilenceConfidence
	}
	return candidate
}

func dropUnassignedAutodetectCandidate(candidate Candidate, hasCorroboratingSource bool) bool {
	if protectedAutodetectCandidate(candidate) {
		return false
	}
	if lineupAssignmentEvidenceScore(candidate) < autodetectLineupOutputMinScore {
		return true
	}
	return hasCorroboratingSource &&
		autodetectSilenceOutputMinDur > 0 &&
		candidate.Duration < autodetectSilenceOutputMinDur &&
		singleSourceSilenceCandidate(candidate)
}

func hasAutodetectCorroborationSource(candidates []Candidate) bool {
	for _, candidate := range candidates {
		for _, source := range candidate.Sources {
			switch source {
			case autodetectSourceAudio, autodetectSourceScene, autodetectSourceColor, autodetectSourceBlack, autodetectSourceFreeze, autodetectSourceOCR:
				return true
			}
		}
	}
	return false
}

func singleSourceSilenceCandidate(candidate Candidate) bool {
	return len(candidate.Sources) == 1 && candidate.Sources[0] == autodetectSourceSilence
}

func pruneNearbyAutodetectDuplicates(candidates []Candidate, spacingSeconds float64) []Candidate {
	if len(candidates) == 0 || spacingSeconds <= 0 {
		return candidates
	}
	pruned := make([]Candidate, 0, len(candidates))
	clusterCenters := make([]float64, 0, len(candidates))
	clusterCounts := make([]int, 0, len(candidates))
	for _, candidate := range candidates {
		candidateTime := autodetectCandidateFusionTime(candidate)
		if len(pruned) == 0 || candidateTime-clusterCenters[len(clusterCenters)-1] > spacingSeconds {
			pruned = append(pruned, candidate)
			clusterCenters = append(clusterCenters, candidateTime)
			clusterCounts = append(clusterCounts, 1)
			continue
		}
		merged, ok := mergeNearbyAutodetectDuplicate(pruned[len(pruned)-1], candidate)
		if !ok {
			pruned = append(pruned, candidate)
			clusterCenters = append(clusterCenters, candidateTime)
			clusterCounts = append(clusterCounts, 1)
			continue
		}
		pruned[len(pruned)-1] = merged
		clusterIndex := len(clusterCenters) - 1
		clusterCounts[clusterIndex]++
		clusterCenters[clusterIndex] += (candidateTime - clusterCenters[clusterIndex]) / float64(clusterCounts[clusterIndex])
	}
	return pruned
}

func mergeNearbyAutodetectDuplicate(a Candidate, b Candidate) (Candidate, bool) {
	aProtected := protectedAutodetectCandidate(a)
	bProtected := protectedAutodetectCandidate(b)
	if aProtected && bProtected {
		if !compatibleProtectedAutodetectDuplicates(a, b) {
			return Candidate{}, false
		}
		if duplicateCandidateRank(b) > duplicateCandidateRank(a) {
			return mergeAutodetectDuplicateMetadata(b, a), true
		}
		return mergeAutodetectDuplicateMetadata(a, b), true
	}
	if aProtected {
		return mergeAutodetectDuplicateMetadata(a, b), true
	}
	if bProtected {
		return mergeAutodetectDuplicateMetadata(b, a), true
	}
	if duplicateCandidateRank(b) > duplicateCandidateRank(a) {
		return mergeAutodetectDuplicateMetadata(b, a), true
	}
	return mergeAutodetectDuplicateMetadata(a, b), true
}

func protectedAutodetectCandidate(candidate Candidate) bool {
	return strings.TrimSpace(candidate.SuggestedName) != "" ||
		candidate.Conflict ||
		sourceContains(candidate.Sources, autodetectSourceOCR)
}

func compatibleProtectedAutodetectDuplicates(a Candidate, b Candidate) bool {
	aName := strings.TrimSpace(a.SuggestedName)
	bName := strings.TrimSpace(b.SuggestedName)
	if aName == "" || bName == "" {
		return true
	}
	return compatibleLineupSuggestion(aName, bName)
}

func duplicateCandidateRank(candidate Candidate) float64 {
	confidence := candidate.Confidence
	if confidence <= 0 {
		confidence = autodetectSilenceConfidence
	}
	return confidence + lineupAssignmentSourceBonus(candidate)
}

func mergeAutodetectDuplicateMetadata(keep Candidate, duplicate Candidate) Candidate {
	merged := keep
	if duplicate.Duration > merged.Duration {
		merged.Duration = duplicate.Duration
	}
	if merged.Status == "" {
		merged.Status = duplicate.Status
	}
	merged.Sources = unionSources(keep.Sources, duplicate.Sources)
	if duplicate.Confidence > merged.Confidence {
		merged.Confidence = duplicate.Confidence
	}
	if merged.SuggestedName == "" {
		merged.SuggestedName = duplicate.SuggestedName
	}
	merged.Conflict = merged.Conflict || duplicate.Conflict
	return merged
}

func compatibleLineupSuggestion(candidateSuggestion string, lineupName string) bool {
	return sameLineupSuggestion(candidateSuggestion, lineupName) || sameLineupSuggestion(lineupName, candidateSuggestion)
}
