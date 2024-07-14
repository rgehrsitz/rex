<script lang="ts">
  import { dashboardStore } from "$lib/stores/dashboardStore";

  $: data = $dashboardStore;
</script>

<div class="system-usage">
  <div class="gauge">
    <h3>CPU Usage</h3>
    <svg viewBox="0 0 100 100">
      <circle
        cx="50"
        cy="50"
        r="45"
        fill="none"
        stroke="#eee"
        stroke-width="10"
      />
      <circle
        cx="50"
        cy="50"
        r="45"
        fill="none"
        stroke="#00aaff"
        stroke-width="10"
        stroke-dasharray="282.7"
        stroke-dashoffset={282.7 - (parseFloat(data.CPUUsage) / 100) * 282.7}
      />
      <text
        x="50"
        y="50"
        text-anchor="middle"
        dominant-baseline="central"
        font-size="20"
      >
        {data.CPUUsage}
      </text>
    </svg>
  </div>
  <div class="gauge">
    <h3>Memory Usage</h3>
    <svg viewBox="0 0 100 100">
      <circle
        cx="50"
        cy="50"
        r="45"
        fill="none"
        stroke="#eee"
        stroke-width="10"
      />
      <circle
        cx="50"
        cy="50"
        r="45"
        fill="none"
        stroke="#ff7700"
        stroke-width="10"
        stroke-dasharray="282.7"
        stroke-dashoffset={282.7 - (parseFloat(data.MemoryUsage) / 100) * 282.7}
      />
      <text
        x="50"
        y="50"
        text-anchor="middle"
        dominant-baseline="central"
        font-size="20"
      >
        {data.MemoryUsage}
      </text>
    </svg>
  </div>
</div>

<style>
  .system-usage {
    display: flex;
    justify-content: space-around;
    margin-bottom: 2rem;
  }

  .gauge {
    width: 200px;
    text-align: center;
  }

  h3 {
    margin-bottom: 1rem;
  }

  svg {
    width: 100%;
    height: auto;
  }

  circle {
    transform: rotate(-90deg);
    transform-origin: 50% 50%;
    transition: stroke-dashoffset 0.3s;
  }
</style>
