package domain

import (
	"sort"
	"time"
)

// TopEquipmentLimit est le nombre d'équipements les plus en panne affichés.
const TopEquipmentLimit = 5

// ComputeStats calcule les indicateurs du tableau de bord en mémoire.
// Le store PostgreSQL calcule les mêmes valeurs en SQL ; les deux
// implémentations doivent rester équivalentes.
func ComputeStats(tickets []*Ticket, equipment []*Equipment, now time.Time) *Stats {
	st := &Stats{TotalTickets: len(tickets)}

	byStatus := map[Status]int{}
	byCat := map[Category]int{}
	failures := map[int64]int{}
	var resSum, respSum time.Duration
	var respCount int

	for _, t := range tickets {
		byStatus[t.Status]++
		byCat[t.Category]++
		if t.EquipmentID != nil {
			failures[*t.EquipmentID]++
		}
		if d, ok := t.ResolutionDuration(); ok {
			resSum += d
			st.ResolvedCount++
		}
		if t.StartedAt != nil {
			respSum += t.StartedAt.Sub(t.CreatedAt)
			respCount++
		}
	}

	for _, s := range []Status{StatusNew, StatusInProgress, StatusDone} {
		st.ByStatus = append(st.ByStatus, StatusCount{Status: s, Count: byStatus[s]})
	}
	for c, n := range byCat {
		st.ByCategory = append(st.ByCategory, CategoryCount{Category: c, Count: n})
	}
	sort.Slice(st.ByCategory, func(i, j int) bool {
		if st.ByCategory[i].Count != st.ByCategory[j].Count {
			return st.ByCategory[i].Count > st.ByCategory[j].Count
		}
		return st.ByCategory[i].Category < st.ByCategory[j].Category
	})
	if st.ResolvedCount > 0 {
		st.AvgResolutionHours = resSum.Hours() / float64(st.ResolvedCount)
	}
	if respCount > 0 {
		st.AvgFirstResponseHours = respSum.Hours() / float64(respCount)
	}

	byID := map[int64]*Equipment{}
	for _, e := range equipment {
		byID[e.ID] = e
		if e.MaintenanceOverdue(now) {
			st.OverdueMaintenance++
		}
	}
	for id, n := range failures {
		e, ok := byID[id]
		if !ok {
			continue
		}
		st.TopEquipment = append(st.TopEquipment, EquipmentFailures{EquipmentID: id, AssetTag: e.AssetTag, Name: e.Name, Count: n})
	}
	sort.Slice(st.TopEquipment, func(i, j int) bool {
		if st.TopEquipment[i].Count != st.TopEquipment[j].Count {
			return st.TopEquipment[i].Count > st.TopEquipment[j].Count
		}
		return st.TopEquipment[i].EquipmentID < st.TopEquipment[j].EquipmentID
	})
	if len(st.TopEquipment) > TopEquipmentLimit {
		st.TopEquipment = st.TopEquipment[:TopEquipmentLimit]
	}
	if st.TopEquipment == nil {
		st.TopEquipment = []EquipmentFailures{}
	}
	if st.ByCategory == nil {
		st.ByCategory = []CategoryCount{}
	}
	return st
}
