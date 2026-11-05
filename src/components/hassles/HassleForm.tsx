import { useState } from "react";
import {
  defaultSchedule,
  intervalUnit,
  reminderStart,
  validateSchedule,
  type HassleSchedule,
  type ReminderStartPreset,
} from "../../lib/hassles/forms";
import Button from "../ui/Button";
import FormField from "../ui/FormField";
import InlineAlert from "../ui/InlineAlert";
import SelectInput from "../ui/SelectInput";
import TextArea from "../ui/TextArea";
import TextInput from "../ui/TextInput";

export default function HassleForm({
  initial,
  busy,
  error,
  onSave,
  onClose,
}: {
  initial?: HassleSchedule;
  busy: boolean;
  error: string;
  onSave: (schedule: HassleSchedule) => void;
  onClose?: () => void;
}) {
  const [schedule, setSchedule] = useState(initial ?? defaultSchedule);
  const [startPreset, setStartPreset] = useState<ReminderStartPreset | "custom">(
    initial ? "custom" : "hour",
  );
  const [validation, setValidation] = useState("");
  const [endMode, setEndMode] = useState(initial?.until ? "date" : "done");
  const change = (patch: Partial<HassleSchedule>) => setSchedule((s) => ({ ...s, ...patch }));
  return (
    <section
      aria-label={initial ? "Edit hassle" : "Add hassle"}
      className="rounded-md border border-line p-4"
    >
      {initial ? <h2 className="mt-0 text-lg">Edit hassle</h2> : null}
      <form
        className="flex flex-col gap-3"
        onSubmit={(event) => {
          event.preventDefault();
          const clean = { ...schedule, title: schedule.title.replace(/[\r\n]+/g, " ").trim() };
          const problem =
            endMode === "date" && clean.cadence !== "once" && !clean.until
              ? "Choose a repeat end date and time."
              : validateSchedule(clean);
          setValidation(problem ?? "");
          if (!problem) onSave(clean);
        }}
      >
        <TextArea
          aria-label="Hassle"
          placeholder="Hassle me about…"
          rows={4}
          required
          value={schedule.title}
          disabled={busy}
          onChange={(e) => change({ title: e.target.value })}
        />
        <FormField label="When · Melbourne" className="flex flex-col gap-1">
          <SelectInput
            value={startPreset}
            disabled={busy}
            onChange={(e) => {
              const preset = e.target.value as ReminderStartPreset | "custom";
              setStartPreset(preset);
              if (preset !== "custom") change({ start: reminderStart(preset) });
            }}
          >
            <option value="hour">In an hour</option>
            <option value="afternoon">This afternoon</option>
            <option value="evening">This evening</option>
            <option value="tomorrow">Tomorrow morning</option>
            <option value="custom">Custom</option>
          </SelectInput>
        </FormField>
        {startPreset === "custom" ? (
          <FormField label="Date and time" className="flex flex-col gap-1">
            <TextInput
              type="datetime-local"
              required
              value={schedule.start}
              disabled={busy}
              onChange={(e) => change({ start: e.target.value })}
            />
          </FormField>
        ) : null}
        <div data-hassle-repeat className="flex min-w-0 items-start gap-3">
          <FormField label="Repeat" className="flex min-w-0 flex-1 flex-col gap-1">
            <SelectInput
              className="w-full min-w-0"
              value={schedule.cadence}
              disabled={busy}
              onChange={(e) => {
                setEndMode("done");
                change({
                  cadence: e.target.value as HassleSchedule["cadence"],
                  interval: 1,
                  until: "",
                });
              }}
            >
              <option value="once">Once</option>
              <option value="daily">Daily</option>
              <option value="weekdays">Weekdays</option>
              <option value="weekly">Weekly</option>
              <option value="monthly">Monthly</option>
              <option value="yearly">Yearly</option>
              <option value="hourly" disabled>
                Hourly (unavailable)
              </option>
            </SelectInput>
          </FormField>
          {schedule.cadence !== "once" && schedule.cadence !== "weekdays" ? (
            <FormField label="Every" className="flex min-w-0 flex-1 flex-col gap-1">
              <div className="flex items-center gap-2">
                <TextInput
                  className="w-full min-w-0"
                  type="number"
                  min={1}
                  max={999}
                  required
                  disabled={busy}
                  value={schedule.interval}
                  onChange={(e) => change({ interval: Number(e.target.value) })}
                />
                <span className="shrink-0 text-sm">
                  {intervalUnit(schedule.cadence, schedule.interval)}
                </span>
              </div>
            </FormField>
          ) : null}
          {schedule.cadence !== "once" ? (
            <FormField label="Until" className="flex min-w-0 flex-1 flex-col gap-1">
              <SelectInput
                className="w-full min-w-0"
                value={endMode}
                disabled={busy}
                onChange={(e) => {
                  setEndMode(e.target.value);
                  if (e.target.value === "done") change({ until: "" });
                }}
              >
                <option value="done">Done</option>
                <option value="date">Date and time</option>
              </SelectInput>
            </FormField>
          ) : null}
        </div>
        {schedule.cadence !== "once" && endMode === "date" ? (
          <FormField label="End date and time" className="flex flex-col gap-1">
            <TextInput
              type="datetime-local"
              required
              value={schedule.until}
              disabled={busy}
              onChange={(e) => change({ until: e.target.value })}
            />
          </FormField>
        ) : null}
        {validation || error ? <InlineAlert>{validation || error}</InlineAlert> : null}
        <div className="flex justify-end gap-2">
          {onClose ? (
            <Button type="button" disabled={busy} onClick={onClose}>
              Cancel
            </Button>
          ) : null}
          <Button type="submit" variant="primary" disabled={busy}>
            {busy ? "Saving…" : initial ? "Save reminder" : "Add hassle"}
          </Button>
        </div>
      </form>
    </section>
  );
}
