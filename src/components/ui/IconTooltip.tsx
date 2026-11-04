import { Tooltip } from "@base-ui/react/tooltip";
import { cloneElement, useContext, type ReactElement } from "react";
import { TooltipsEnabled } from "./TooltipProvider";

export default function IconTooltip({
  children,
}: {
  children: ReactElement<{ title?: string; disabled?: boolean }>;
}) {
  const enabled = useContext(TooltipsEnabled);
  const { title, disabled } = children.props;
  if (!enabled || !title || disabled) return children;
  return (
    <Tooltip.Root>
      <Tooltip.Trigger render={cloneElement(children, { title: undefined })} />
      <Tooltip.Portal>
        <Tooltip.Positioner sideOffset={6} className="z-70">
          <Tooltip.Popup
            data-icon-tooltip
            className="max-w-72 rounded-md border border-line bg-card px-2 py-1 text-xs text-fg shadow-md"
          >
            {title}
          </Tooltip.Popup>
        </Tooltip.Positioner>
      </Tooltip.Portal>
    </Tooltip.Root>
  );
}
