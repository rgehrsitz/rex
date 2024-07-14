<script lang="ts">
  import { onMount } from "svelte";
  import Chart from "chart.js/auto";
  import type { ChartConfiguration } from "chart.js";
  import {
    dashboardStore,
    type DashboardData,
  } from "$lib/stores/dashboardStore";

  let chartCanvas: HTMLCanvasElement;
  let chart: Chart;
  let ruleData: number[] = [];

  $: data = $dashboardStore;

  onMount(() => {
    const config: ChartConfiguration = {
      type: "line",
      data: {
        labels: [],
        datasets: [
          {
            label: "Rules Executed per Second",
            data: ruleData,
            borderColor: "#4caf50",
            tension: 0.4,
          },
        ],
      },
      options: {
        responsive: true,
        scales: {
          y: {
            beginAtZero: true,
          },
        },
      },
    };

    chart = new Chart(chartCanvas, config);

    return () => {
      chart.destroy();
    };
  });

  let lastTotalRules = 0;
  let lastUpdateTime: number | null = null;

  $: if (chart && data) {
    const currentTime = Date.now();
    if (lastUpdateTime) {
      const timeElapsed = (currentTime - lastUpdateTime) / 1000; // in seconds
      const rulesExecuted = data.TotalRulesProcessed - lastTotalRules;
      const rate = rulesExecuted / timeElapsed;

      ruleData.push(rate);
      if (ruleData.length > 20) ruleData.shift();

      chart.data.labels = ruleData.map((_, i) => i.toString());
      chart.data.datasets[0].data = ruleData;
      chart.update();
    }
    lastTotalRules = data.TotalRulesProcessed;
    lastUpdateTime = currentTime;
  }
</script>

<div class="chart-container">
  <h3>Rule Execution Rate</h3>
  <canvas bind:this={chartCanvas}></canvas>
</div>

<style>
  .chart-container {
    width: 100%;
    max-width: 600px;
    margin: 0 auto 2rem;
  }

  h3 {
    text-align: center;
    margin-bottom: 1rem;
  }
</style>
