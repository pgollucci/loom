package taskexecutor

import (
	"github.com/jordanhubbard/loom/internal/orgchart"
	"github.com/jordanhubbard/loom/pkg/models"
)

// OrgChartAdapter adapts the orgchart.Manager to the OrgChartProvider interface.
type OrgChartAdapter struct {
	manager *orgchart.Manager
}

// NewOrgChartAdapter wraps an orgchart.Manager.
func NewOrgChartAdapter(m *orgchart.Manager) *OrgChartAdapter {
	return &OrgChartAdapter{manager: m}
}

func (a *OrgChartAdapter) GetOrgChart(projectID string) *models.OrgChart {
	if a.manager == nil {
		return nil
	}
	chart, err := a.manager.GetByProject(projectID)
	if err != nil {
		return nil
	}
	return chart
}

func (a *OrgChartAdapter) GetDefaultOrgChart() *models.OrgChart {
	if a.manager == nil {
		return nil
	}
	return a.manager.GetDefaultTemplate()
}
