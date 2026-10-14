import { test, expect } from "@playwright/test";

for (const [mode, base] of [["dev", "http://127.0.0.1:5173/"], ["submission", "http://127.0.0.1:4174/#/"]] as const) {
  test(`${mode}: account menu supports keyboard, navigation, dismissal and logout`, async ({ page }) => {
    await page.goto(base);
    const trigger = page.getByRole("button", { name: "Меню пользователя", exact: true });
    const menu = page.getByRole("menu", { name: "Действия пользователя" });
    await trigger.focus();
    await page.keyboard.press("ArrowDown");
    await expect(menu.getByRole("menuitem", { name: "Мой профиль" })).toBeFocused();
    await page.keyboard.press("ArrowDown");
    await expect(menu.getByRole("menuitem", { name: "Мои материалы" })).toBeFocused();
    await page.keyboard.press("Escape");
    await expect(menu).toHaveCount(0);
    await expect(trigger).toBeFocused();
    await trigger.click();
    await page.getByRole("heading", { name: "Библиотека знаний." }).click();
    await expect(menu).toHaveCount(0);
    await trigger.click();
    await menu.getByRole("menuitem", { name: "Мой профиль" }).click();
    await expect(page.getByRole("heading", { name: "Алексей Смирнов" })).toBeVisible();
    await expect(menu).toHaveCount(0);
    await trigger.click();
    await menu.getByRole("menuitem", { name: "Избранное" }).click();
    await expect(page).toHaveURL(/\/saved$/);
    await trigger.click();
    await menu.getByRole("menuitem", { name: "Выйти", exact: true }).click();
    await expect(page).toHaveURL(/\/login$/);
    await page.reload();
    await expect(page).toHaveURL(/\/login$/);
    await expect(trigger).toHaveCount(0);
  });

  test(`${mode}: dark theme persists and covers reader, editor and mobile menu`, async ({ page }) => {
    const errors: string[] = [];
    page.on("pageerror", error => errors.push(error.message));
    await page.goto(base);
    const trigger = page.getByRole("button", { name: "Меню пользователя", exact: true });
    await trigger.click();
    await page.getByRole("menuitemcheckbox", { name: "Тёмная тема" }).click();
    await expect(page.getByRole("menuitemcheckbox", { name: "Тёмная тема" })).toBeChecked();
    await page.keyboard.press("Escape");
    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
    await expect(page.locator(".material-card")).toHaveCount(8);
    await expect(page.locator(".sidebar")).toHaveCSS("background-color", "rgb(29, 29, 43)");
    if (mode === "submission") {
      await trigger.click();
      await expect(page.locator(".account-panel")).toHaveCSS("opacity", "1");
      await page.screenshot({ path: "../docs/assignment-2/screenshots/09-dark-account-menu.png" });
      await page.keyboard.press("Escape");
    }
    const visit = (route: string) => page.goto(mode === "submission" ? `http://127.0.0.1:4174/#${route}` : `http://127.0.0.1:5173${route}`);
    for (const route of ["/notes/note-2", "/new", "/ranking", "/moderation", "/profile"]) {
      await visit(route);
      await expect(page.locator("h1").first()).toBeVisible();
      await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
      if (mode === "submission" && route === "/notes/note-2") await page.screenshot({ path: "../docs/assignment-2/screenshots/10-dark-reader.png", fullPage: true });
    }
    await visit("/");
    await page.setViewportSize({ width: 390, height: 844 });
    await trigger.click();
    await expect(page.getByRole("menu", { name: "Действия пользователя" })).toBeVisible();
    await expect(page.locator(".account-panel")).toHaveCSS("opacity", "1");
    await expect.poll(async () => (await page.locator(".sidebar").boundingBox())?.x).toBeLessThanOrEqual(-234);
    await expect.poll(() => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    if (mode === "submission") await page.screenshot({ path: "../docs/assignment-2/screenshots/11-dark-mobile-menu.png" });
    await page.getByRole("menuitemcheckbox", { name: "Тёмная тема" }).click();
    await page.reload();
    await expect(page.locator("html")).toHaveAttribute("data-theme", "light");
    expect(errors).toEqual([]);
  });
}
