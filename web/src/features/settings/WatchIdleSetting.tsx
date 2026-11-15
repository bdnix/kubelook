const MIN = 30;
const MAX = 300;

function describe(seconds: number): string {
  if (seconds < 60) return `${seconds} seconds`;
  const minutes = seconds / 60;
  const text = Number.isInteger(minutes) ? String(minutes) : minutes.toFixed(1);
  return `${text} ${minutes === 1 ? 'minute' : 'minutes'}`;
}

export interface WatchIdleSettingProps {
  seconds: number;
  onChange: (seconds: number) => void;
}

/** How long a hidden tab keeps its live lists before they pause: 30 seconds to 5 minutes. */
export function WatchIdleSetting({ seconds, onChange }: WatchIdleSettingProps) {
  return (
    <div className="watch-idle">
      <label>
        Pause hidden live lists after
        <input
          type="range"
          min={MIN}
          max={MAX}
          step={10}
          value={seconds}
          onChange={(e) => onChange(Number(e.target.value))}
        />
      </label>
      <output>{describe(seconds)}</output>
    </div>
  );
}
