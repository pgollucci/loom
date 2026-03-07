<template>
  <div class="performance-reviews">
    <div class="grade-distribution-section">
      <h2>Grade Distribution</h2>
      <div class="grade-chart-container">
        <div id="grade-chart" class="grade-chart"></div>
      </div>
      <div class="summary-cards">
        <div class="summary-card warning">
          <div class="card-value">{{ agentsOnWarning }}</div>
          <div class="card-label">Agents on Warning</div>
        </div>
        <div class="summary-card at-risk">
          <div class="card-value">{{ agentsAtRisk }}</div>
          <div class="card-label">At Risk of Firing</div>
        </div>
        <div class="summary-card promotion">
          <div class="card-value">{{ agentsEligiblePromotion }}</div>
          <div class="card-label">Eligible for Promotion</div>
        </div>
      </div>
    </div>
    <div class="agent-table-section">
      <h2>Agent Performance</h2>
      <div class="table-controls">
        <input v-model="filterText" type="text" placeholder="Filter by name or role..." class="filter-input" />
        <select v-model="filterGrade" class="filter-select">
          <option value="">All Grades</option>
          <option value="A">A</option>
          <option value="B">B</option>
          <option value="C">C</option>
          <option value="D">D</option>
          <option value="F">F</option>
        </select>
      </div>
      <table class="agent-table">
        <thead>
          <tr>
            <th>Display Name</th>
            <th>Role</th>
            <th>Current Grade</th>
            <th>Last 3 Grades</th>
            <th>Beads Closed</th>
            <th>Beads Blocked</th>
            <th>Efficiency</th>
            <th>Status</th>
            <th>Actions</th>
          </tr>
        </thead>
        <tbody>
          <template v-for="agent in filteredAgents" :key="agent.id">
            <tr class="agent-row" @click="toggleExpanded(agent.id)">
              <td>{{ agent.displayName }}</td>
              <td>{{ agent.role }}</td>
              <td class="grade" :class="'grade-' + agent.currentGrade">{{ agent.currentGrade }}</td>
              <td class="trend">
                <span v-for="(grade, idx) in agent.last3Grades" :key="idx" class="grade-badge" :class="'grade-' + grade">
                  {{ grade }}
                </span>
              </td>
              <td>{{ agent.beadsClosedCount }}</td>
              <td>{{ agent.beadsBlockedCount }}</td>
              <td>{{ agent.efficiencyPercent }}%</td>
              <td class="status" :class="agent.status">{{ agent.status }}</td>
              <td class="actions">
                <button @click.stop="showActions(agent.id)" class="action-btn">Menu</button>
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
  </div>
</template>

<script>
export default {
  name: 'PerformanceReviews',
  data() {
    return {
      agents: [],
      filterText: '',
      filterGrade: '',
    };
  },
  computed: {
    filteredAgents() {
      return this.agents.filter(agent => {
        const matchesText = agent.displayName.toLowerCase().includes(this.filterText.toLowerCase());
        const matchesGrade = !this.filterGrade || agent.currentGrade === this.filterGrade;
        return matchesText && matchesGrade;
      });
    },
    agentsOnWarning() {
      return this.agents.filter(a => a.status === 'warning').length;
    },
    agentsAtRisk() {
      return this.agents.filter(a => a.status === 'at_risk').length;
    },
    agentsEligiblePromotion() {
      return this.agents.filter(a => a.status === 'eligible_promotion').length;
    },
  },
  methods: {
    async loadPerformanceReviews() {
      try {
        const response = await fetch('/api/v1/performance-reviews');
        const data = await response.json();
        this.agents = data.agents || [];
      } catch (error) {
        console.error('Failed to load performance reviews:', error);
      }
    },
    toggleExpanded(agentId) {
      console.log('Expand agent:', agentId);
    },
    showActions(agentId) {
      console.log('Show actions for:', agentId);
    },
  },
  mounted() {
    this.loadPerformanceReviews();
  },
};
</script>

<style scoped>
.performance-reviews { padding: 20px; background: #f5f5f5; }
.grade-distribution-section { background: white; border-radius: 8px; padding: 20px; margin-bottom: 30px; }
.summary-cards { display: grid; grid-template-columns: repeat(3, 1fr); gap: 15px; }
.summary-card { padding: 15px; border-radius: 6px; text-align: center; color: white; }
.summary-card.warning { background: #ff9800; }
.summary-card.at-risk { background: #f44336; }
.summary-card.promotion { background: #4caf50; }
.card-value { font-size: 32px; font-weight: bold; }
.agent-table-section { background: white; border-radius: 8px; padding: 20px; }
.table-controls { display: flex; gap: 10px; margin-bottom: 20px; }
.filter-input, .filter-select { padding: 8px 12px; border: 1px solid #ddd; border-radius: 4px; }
.agent-table { width: 100%; border-collapse: collapse; }
.agent-table th, .agent-table td { padding: 12px; text-align: left; border-bottom: 1px solid #ddd; }
.agent-table th { background: #f9f9f9; font-weight: bold; }
.grade { font-weight: bold; }
.grade-A { color: #4caf50; }
.grade-B { color: #2196f3; }
.grade-C { color: #ff9800; }
.grade-D { color: #f44336; }
.grade-F { color: #9c27b0; }
</style>