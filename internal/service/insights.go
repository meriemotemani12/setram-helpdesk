package service

import (
	"context"
	"sort"
	"strings"
	"unicode"
)

// AIInsights contains a staff-only overview of recurring support issues.
// The diagnostic steps are rule-based suggestions, not an automated diagnosis.
type AIInsights struct {
	TicketCount     int               `json:"ticket_count"`
	OpenCount       int               `json:"open_count"`
	ByCategory      []InsightCategory `json:"by_category"`
	RecurringIssues []RecurringIssue  `json:"recurring_issues"`
	DiagnosticSteps []DiagnosticSteps `json:"diagnostic_steps"`
}

type InsightCategory struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

type RecurringIssue struct {
	Title string `json:"title"`
	Count int    `json:"count"`
}

type DiagnosticSteps struct {
	Category string   `json:"category"`
	Steps    []string `json:"steps"`
}

func (s *Service) AIInsights(ctx context.Context, a Actor) (*AIInsights, error) {
	if err := requireStaff(a); err != nil {
		return nil, err
	}

	// Analyze the 500 most recent tickets to keep the request bounded.
	tickets, err := s.store.ListTickets(ctx, TicketFilter{Limit: 500})
	if err != nil {
		return nil, err
	}

	result := &AIInsights{
		TicketCount:     len(tickets),
		ByCategory:      []InsightCategory{},
		RecurringIssues: []RecurringIssue{},
		DiagnosticSteps: []DiagnosticSteps{},
	}

	categoryCounts := make(map[string]int)
	issueCounts := make(map[string]int)
	for _, ticket := range tickets {
		if ticket == nil {
			continue
		}

		category := string(ticket.Category)
		categoryCounts[category]++

		if string(ticket.Status) != "done" {
			result.OpenCount++
		}

		issue := classifyRecurringIssue(string(ticket.Category), ticket.Title+" "+ticket.Description)
		if issue != "" {

			issueCounts[issue]++

		}
	}

	for category, count := range categoryCounts {
		result.ByCategory = append(result.ByCategory, InsightCategory{
			Category: category,
			Count:    count,
		})
	}
	sort.Slice(result.ByCategory, func(i, j int) bool {
		if result.ByCategory[i].Count == result.ByCategory[j].Count {
			return result.ByCategory[i].Category < result.ByCategory[j].Category
		}
		return result.ByCategory[i].Count > result.ByCategory[j].Count
	})
	for issue, count := range issueCounts {
		if count > 1 {
			result.RecurringIssues = append(result.RecurringIssues, RecurringIssue{
				Title: issue,
				Count: count,
			})
		}
	}

	sort.Slice(result.RecurringIssues, func(i, j int) bool {
		if result.RecurringIssues[i].Count == result.RecurringIssues[j].Count {
			return result.RecurringIssues[i].Title < result.RecurringIssues[j].Title
		}
		return result.RecurringIssues[i].Count > result.RecurringIssues[j].Count
	})
	if len(result.RecurringIssues) > 5 {
		result.RecurringIssues = result.RecurringIssues[:5]
	}

	stepsByCategory := map[string][]string{
		"computer": {
			"Vérifier l'alimentation et les câbles.",
			"Redémarrer le poste si cela est approprié.",
			"Relever le message d'erreur et vérifier les mises à jour récentes.",
		},
		"network": {
			"Vérifier la connexion Wi-Fi ou le câble réseau.",
			"Tester la connectivité et vérifier la configuration IP.",
			"Comparer avec un autre poste pour déterminer l'étendue de la panne.",
		},
		"printer": {
			"Vérifier l'alimentation, le papier et les voyants.",
			"Contrôler la connexion et la file d'impression.",
			"Imprimer une page de test et relever le code d'erreur éventuel.",
		},
		"ticketing_counter": {
			"Vérifier l'alimentation et les connexions du poste de vente.",
			"Contrôler l'accès à l'application et au réseau.",
			"Noter le message d'erreur, l'heure et les opérations concernées.",
		},
		"other": {
			"Préciser les symptômes et les étapes permettant de reproduire le problème.",
			"Noter le message d'erreur et l'heure de l'incident.",
			"Vérifier si d'autres utilisateurs ou équipements sont concernés.",
		},
	}

	for _, item := range result.ByCategory {
		if steps, ok := stepsByCategory[item.Category]; ok {
			result.DiagnosticSteps = append(result.DiagnosticSteps, DiagnosticSteps{
				Category: item.Category,
				Steps:    steps,
			})
		}
	}

	return result, nil
}

func normalizeIssueTitle(title string) string {
	var b strings.Builder
	space := false

	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		if unicode.IsSpace(r) {
			if !space {
				b.WriteByte(' ')
				space = true
			}
			continue
		}
		b.WriteRune(r)
		space = false
	}

	return strings.TrimSpace(b.String())
}
func classifyRecurringIssue(category, text string) string {
	text = normalizeIssueTitle(text)

	keywords := map[string][]string{
		"network":           {"wifi", "wi-fi", "internet", "reseau", "connexion"},
		"computer":          {"ordinateur", "pc", "ecran", "clavier", "souris", "demarrage"},
		"printer":           {"imprimante", "impression", "papier", "toner"},
		"ticketing_counter": {"billet", "billetterie", "vente", "guichet", "ticket"},
	}

	for _, keyword := range keywords[category] {
		if strings.Contains(text, keyword) {
			return category + ": " + keyword
		}
	}

	return ""
}
