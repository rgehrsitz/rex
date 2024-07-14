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
  let factData: number[] = [];

  $: data = $dashboardStore;

  onMount(() => {
    const config: ChartConfiguration = {
      type: "line",
      data: {
        labels: [],
        datasets: [
          {
            label: "Facts Processed per Second",
            data: factData,
            borderColor: "#2196f3",
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

  let lastTotalFacts = 0;
  let lastUpdateTime: number | null = null;

  $: if (chart && data) {
    const currentTime = Date.now();
    if (lastUpdateTime) {
      const timeElapsed = (currentTime - lastUpdateTime) / 1000; // in seconds
      const factsProcessed = data.TotalFactsProcessed - lastTotalFacts;
      const rate = factsProcessed / timeElapsed;

      factData.push(rate);
      if (factData.length > 20) factData.shift();

      chart.data.labels = factData.map((_, i) => i.toString());
      chart.data.datasets[0].data = factData;
      chart.update();
    }
    lastTotalFacts = data.TotalFactsProcessed;
    lastUpdateTime = currentTime;
  }
</script>

<div class="chart-container">
  <h3>Fact Processing Rate</h3>
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
