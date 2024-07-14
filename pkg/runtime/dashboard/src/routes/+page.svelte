<script lang="ts">
  import { onMount } from "svelte";
  import { dashboardStore } from "$lib/stores/dashboardStore";
  import SummaryStats from "$lib/components/SummaryStats.svelte";
  import SystemUsage from "$lib/components/SystemUsage.svelte";
  import RuleEvaluationChart from "$lib/components/RuleEvaluationChart.svelte";
  import ErrorWarningCounters from "$lib/components/ErrorWarningCounters.svelte";
  import FactProcessingChart from "$lib/components/FactProcessingChart.svelte";
  import RuleExecutionChart from "$lib/components/RuleExecutionChart.svelte";

  onMount(() => {
    const ws = new WebSocket(`ws://${window.location.host}/events`);

    ws.onmessage = (event) => {
      const data = JSON.parse(event.data);
      dashboardStore.set(data);
    };

    return () => {
      ws.close();
    };
  });
</script>

<main>
  <h1>REX Dashboard</h1>
  <SummaryStats />
  <SystemUsage />
  <ErrorWarningCounters />
  <div class="chart-grid">
    <RuleEvaluationChart />
    <FactProcessingChart />
    <RuleExecutionChart />
  </div>
</main>

<style>
  main {
    padding: 1rem;
    max-width: 1200px;
    margin: 0 auto;
  }

  h1 {
    color: #333;
    text-align: center;
    margin-bottom: 2rem;
  }

  .chart-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
    gap: 1rem;
  }
</style>
