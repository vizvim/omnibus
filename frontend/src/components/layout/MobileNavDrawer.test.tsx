import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { axe } from "jest-axe";
import { describe, expect, it, vi } from "vitest";

import { MobileNavDrawer } from "./MobileNavDrawer";
import { NAV_ITEMS } from "./nav";

// renderDrawer mounts the drawer with a no-op navigate handler. The drawer owns a single
// hamburger trigger; the panel + nav are portalled by Radix Dialog when opened.
function renderDrawer(onNavigate: (route: string) => void = () => {}) {
  return render(
    <MobileNavDrawer active="library" onNavigate={onNavigate as never} />,
  );
}

describe("MobileNavDrawer", () => {
  it("exposes a labelled hamburger trigger that is hidden at lg+ (lg:hidden)", () => {
    renderDrawer();
    const trigger = screen.getByRole("button", { name: /open navigation/i });
    expect(trigger).toBeInTheDocument();
    // Responsive contract: the trigger only shows below lg; the persistent Sidebar covers lg+.
    expect(trigger.className).toContain("lg:hidden");
  });

  it("opens the drawer from the hamburger and renders the SAME NAV_ITEMS source", async () => {
    renderDrawer();
    fireEvent.click(screen.getByRole("button", { name: /open navigation/i }));

    // The dialog opens with an accessible name and every nav label from the shared source.
    const dialog = await screen.findByRole("dialog");
    expect(dialog).toBeInTheDocument();
    for (const item of NAV_ITEMS) {
      expect(
        screen.getByRole("button", { name: new RegExp(item.label, "i") }),
      ).toBeInTheDocument();
    }
    // No forked list: the count of rendered nav buttons matches the shared source exactly.
    const navButtons = screen
      .getAllByRole("button")
      .filter((b) => NAV_ITEMS.some((i) => new RegExp(`^${i.label}$`, "i").test(b.textContent ?? "")));
    expect(navButtons).toHaveLength(NAV_ITEMS.length);
  });

  it("marks the active route with aria-current=page", async () => {
    render(<MobileNavDrawer active="settings" onNavigate={() => {}} />);
    fireEvent.click(screen.getByRole("button", { name: /open navigation/i }));
    await screen.findByRole("dialog");
    const settings = screen.getByRole("button", { name: /settings/i });
    expect(settings).toHaveAttribute("aria-current", "page");
  });

  it("navigates and closes the drawer when a nav item is selected", async () => {
    const onNavigate = vi.fn();
    renderDrawer(onNavigate);
    fireEvent.click(screen.getByRole("button", { name: /open navigation/i }));
    await screen.findByRole("dialog");

    fireEvent.click(screen.getByRole("button", { name: /discover/i }));
    expect(onNavigate).toHaveBeenCalledWith("search");
    // Selecting an item closes the drawer.
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });

  it("closes on ESC and restores focus to the trigger (Radix focus management)", async () => {
    renderDrawer();
    const trigger = screen.getByRole("button", { name: /open navigation/i });
    fireEvent.click(trigger);
    const dialog = await screen.findByRole("dialog");

    fireEvent.keyDown(dialog, { key: "Escape" });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    // Radix restores focus to the trigger after the dialog closes.
    await waitFor(() => expect(trigger).toHaveFocus());
  });

  it("has no axe violations when open", async () => {
    const { container } = renderDrawer();
    fireEvent.click(screen.getByRole("button", { name: /open navigation/i }));
    await screen.findByRole("dialog");
    expect(await axe(container)).toHaveNoViolations();
  });
});
