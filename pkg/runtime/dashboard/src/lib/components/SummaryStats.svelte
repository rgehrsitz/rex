<script lang="ts">
  import { dashboardStore } from "$lib/stores/dashboardStore";

  $: data = $dashboardStore;
</script>

<div class="summary-stats">
  <div class="stat">
    <h3>Facts Processed</h3>
    <p>{data.TotalFactsProcessed}</p>
  </div>
  <div class="stat">
    <h3>Rules Processed</h3>
    <p>{data.TotalRulesProcessed}</p>
  </div>
  <div class="stat">
    <h3>Facts Updated</h3>
    <p>{data.TotalFactsUpdated}</p>
  </div>
  <div class="stat">
    <h3>Engine Uptime</h3>
    <p>{data.EngineUptime}</p>
  </div>
</div>

<style>
  .summary-stats {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
    gap: 1rem;
    margin-bottom: 2rem;
  }

  .stat {
    background-color: #f0f0f0;
    padding: 1rem;
    border-radius: 8px;
    text-align: center;
  }

  h3 {
    margin: 0 0 0.5rem 0;
    font-size: 1rem;
    color: #555;
  }

  p {
    margin: 0;
    font-size: 1.5rem;
    font-weight: bold;
    color: #333;
  }
</style>
