import { useEffect, useRef, useState, useCallback } from "react";

const STORAGE_KEY = "certship_refresh_interval";
const DEFAULT_INTERVAL = 0; // 0 = disabled

export function useAutoRefresh(fetchData: () => void) {
  const [interval, setIntervalValue] = useState<number>(() => {
    const saved = localStorage.getItem(STORAGE_KEY);
    return saved ? Number(saved) : DEFAULT_INTERVAL;
  });
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null);

  const setInterval_ = useCallback((val: number) => {
    setIntervalValue(val);
    localStorage.setItem(STORAGE_KEY, String(val));
  }, []);

  useEffect(() => {
    if (timerRef.current) {
      clearInterval(timerRef.current);
      timerRef.current = null;
    }
    if (interval > 0) {
      timerRef.current = setInterval(fetchData, interval * 1000);
    }
    return () => {
      if (timerRef.current) clearInterval(timerRef.current);
    };
  }, [interval, fetchData]);

  return { interval, setInterval: setInterval_ };
}

export const REFRESH_OPTIONS = [
  { value: 0, label: "关闭自动刷新" },
  { value: 5, label: "5 秒" },
  { value: 10, label: "10 秒" },
  { value: 30, label: "30 秒" },
  { value: 60, label: "1 分钟" },
  { value: 300, label: "5 分钟" },
];
