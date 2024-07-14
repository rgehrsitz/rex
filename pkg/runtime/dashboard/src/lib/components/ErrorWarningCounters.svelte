<script lang="ts">
  import { dashboardStore } from "$lib/stores/dashboardStore";

  $: data = $dashboardStore;
</script>

<div class="counters">
  <div class="counter error">
    <h3>Errors</h3>
    <p>{data.ErrorCount}</p>
  </div>
  <div class="counter warning">
    <h3>Warnings</h3>
    <p>{data.WarningCount}</p>
  </div>
</div>

<style>
  .counters {
    display: flex;
    justify-content: space-around;
    margin-bottom: 2rem;
  }

  .counter {
    text-align: center;
    padding: 1rem;
    border-radius: 8px;
    width: 150px;
  }

  .error {
    background-color: #ffebee;
    color: #c62828;
  }

  .warning {
    background-color: #fff8e1;
    color: #f9a825;
  }

  h3 {
    margin: 0 0 0.5rem 0;
  }

  p {
    font-size: 2rem;
    margin: 0;
    font-weight: bold;
  }
</style>
