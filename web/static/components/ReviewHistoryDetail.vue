<template>
  <div class="review-history-detail">
    <h3>Review History for {{ agent.displayName }}</h3>
    <div class="history-timeline">
      <div v-for="cycle in history.cycles" :key="cycle.cycle" class="timeline-item">
        <div class="timeline-marker" :class="'grade-' + cycle.grade">{{ cycle.grade }}</div>
        <div class="timeline-content">
          <h4>{{ cycle.cycle }}</h4>
          <div class="cycle-stats">
            <div class="stat">
              <span class="stat-label">Completion:</span>
              <span class="stat-value">{{ cycle.completion_percent }}%</span>
            </div>
            <div class="stat">
              <span class="stat-label">Efficiency:</span>
              <span class="stat-value">{{ cycle.efficiency_percent }}%</span>
            </div>
            <div class="stat">
              <span class="stat-label">Assist Credits:</span>
              <span class="stat-value">{{ cycle.assist_credits }}</span>
            </div>
            <div class="stat">
              <span class="stat-label">Beads Closed:</span>
              <span class="stat-value">{{ cycle.beads_closed }}</span>
            </div>
            <div class="stat">
              <span class="stat-label">Beads Blocked:</span>
              <span class="stat-value">{{ cycle.beads_blocked }}</span>
            </div>
          </div>
        </div>
      </div>
    </div>
    <div class="events-section">
      <h4>Events</h4>
      <div v-for="event in history.events" :key="event.timestamp" class="event-item">
        <span class="event-type" :class="event.type">{{ event.type }}</span>
        <span class="event-description">{{ event.description }}</span>
        <span class="event-timestamp">{{ formatDate(event.timestamp) }}</span>
      </div>
    </div>
  </div>
</template>

<script>
export default {
  name: 'ReviewHistoryDetail',
  props: {
    agentId: String,
    agent: Object,
  },
  data() {
    return {
      history: {
        cycles: [],
        events: [],
      },
    };
  },
  methods: {
    async loadHistory() {
      try {
        const response = await fetch(`/api/v1/performance-reviews/${this.agentId}/history`);
        const data = await response.json();
        this.history = data;
      } catch (error) {
        console.error('Failed to load review history:', error);
      }
    },
    formatDate(timestamp) {
      return new Date(timestamp).toLocaleDateString();
    },
  },
  mounted() {
    this.loadHistory();
  },
};
</script>

<style scoped>
.review-history-detail { padding: 20px; background: #fafafa; border-radius: 6px; }
.history-timeline { margin-bottom: 30px; }
.timeline-item { display: flex; gap: 20px; margin-bottom: 20px; }
.timeline-marker { width: 50px; height: 50px; border-radius: 50%; display: flex; align-items: center; justify-content: center; color: white; font-weight: bold; flex-shrink: 0; }
.timeline-marker.grade-A { background: #4caf50; }
.timeline-marker.grade-B { background: #2196f3; }
.timeline-marker.grade-C { background: #ff9800; }
.timeline-marker.grade-D { background: #f44336; }
.timeline-marker.grade-F { background: #9c27b0; }
.timeline-content { flex: 1; }
.timeline-content h4 { margin: 0 0 10px 0; color: #333; }
.cycle-stats { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 10px; }
.stat { display: flex; justify-content: space-between; padding: 8px; background: white; border-radius: 4px; }
.stat-label { font-weight: 500; color: #666; }
.stat-value { color: #333; font-weight: bold; }
.events-section { margin-top: 20px; }
.events-section h4 { margin: 0 0 15px 0; color: #333; }
.event-item { display: flex; gap: 15px; padding: 10px; background: white; border-radius: 4px; margin-bottom: 10px; align-items: center; }
.event-type { padding: 4px 8px; border-radius: 3px; font-size: 12px; font-weight: bold; color: white; }
.event-type.warning_issued { background: #ff9800; }
.event-type.self_optimization { background: #2196f3; }
.event-type.fired { background: #f44336; }
.event-description { flex: 1; color: #666; }
.event-timestamp { color: #999; font-size: 12px; }
</style>