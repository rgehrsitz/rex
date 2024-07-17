// pkg\runtime\dashboard\src\lib\stores\dashboardStore.ts

import { writable } from "svelte/store";

export interface DashboardData {
  TotalFactsProcessed: number;
  TotalRulesProcessed: number;
  TotalFactsUpdated: number;
  LastUpdateTime: string;
  EngineUptime: string;
  CPUUsage: string;
  MemoryUsage: string;
  GoroutineCount: number;
  ErrorCount: number;
  WarningCount: number;
  TotalRules: number;
  TotalFacts: number;
  AverageRuleEvaluationTime?: number; // Add this line
}

const initialData: DashboardData = {
  TotalFactsProcessed: 0,
  TotalRulesProcessed: 0,
  TotalFactsUpdated: 0,
  LastUpdateTime: "",
  EngineUptime: "",
  CPUUsage: "0%",
  MemoryUsage: "0 MB",
  GoroutineCount: 0,
  ErrorCount: 0,
  WarningCount: 0,
  TotalRules: 0,
  TotalFacts: 0,
  AverageRuleEvaluationTime: 0, // Add this line
};

export const dashboardStore = writable<DashboardData>(initialData);
