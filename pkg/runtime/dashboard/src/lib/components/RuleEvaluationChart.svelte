<script lang="ts">
  import { onMount } from "svelte";
  import Chart from "chart.js/auto";
  import type { ChartConfiguration, ChartData } from "chart.js";
  import {
    dashboardStore,
    type DashboardData,
  } from "$lib/stores/dashboardStore";

  let chartCanvas: HTMLCanvasElement;
  let chart: Chart;

  $: data = $dashboardStore;

  onMount(() => {
    const config: ChartConfiguration = {
      type: "bar",
      data: {
        labels: ["Rule Evaluation Time"],
        datasets: [
          {
            label: "Time (ms)",
            data: [0],
            backgroundColor: "#4caf50",
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

  $: if (chart && data) {
    const chartData: ChartData = {
      labels: ["Rule Evaluation Time"],
      datasets: [
        {
          label: "Time (ms)",
          data: [data.AverageRuleEvaluationTime || 0],
          backgroundColor: "#4caf50",
        },
      ],
    };
    chart.data = chartData;
    chart.update();
  }
</script>

<div class="chart-container">
  <h3>Average Rule Evaluation Time</h3>
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
