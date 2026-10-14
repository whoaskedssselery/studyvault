import { useEffect, useRef, useState } from "react";
import { Bookmark, ChevronDown, FileText, LogOut, Moon, Sun, UserRound } from "lucide-react";
import type { User } from "./data";
import type { Theme } from "./theme";

export function AccountMenu({ user, navigate, logout, theme, toggleTheme }: {
  user: User;
  navigate: (path: string) => void;
  logout: () => void;
  theme: Theme;
  toggleTheme: () => void;
}) {
  const [open, setOpen] = useState(false);
  const container = useRef<HTMLDivElement>(null);
  const trigger = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const frame = requestAnimationFrame(() => panel.current?.querySelector<HTMLButtonElement>("button")?.focus());
    const outside = (event: Event) => {
      if (event.target instanceof Node && !container.current?.contains(event.target)) setOpen(false);
    };
    const escape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        setOpen(false);
        trigger.current?.focus();
      }
    };
    document.addEventListener("pointerdown", outside);
    document.addEventListener("focusin", outside);
    document.addEventListener("keydown", escape);
    return () => {
      cancelAnimationFrame(frame);
      document.removeEventListener("pointerdown", outside);
      document.removeEventListener("focusin", outside);
      document.removeEventListener("keydown", escape);
    };
  }, [open]);
  const visit = (path: string) => { setOpen(false); navigate(path); };
  return <div className="account-menu" ref={container}>
    <button ref={trigger} className="user-button" aria-label="Меню пользователя" aria-haspopup="menu" aria-expanded={open} aria-controls={open ? "account-menu-panel" : undefined}
      onClick={() => setOpen(value => !value)} onKeyDown={event => {
        if (event.key === "ArrowDown") { event.preventDefault(); setOpen(true); }
      }}>
      <span className={`avatar ${user.color}`}>{user.initials}</span>
      <span>{user.name.split(" ")[0]}<ChevronDown size={14} className={open ? "account-chevron open" : "account-chevron"}/></span>
    </button>
    {open && <div className="account-panel" ref={panel}>
      <div className="account-identity"><strong>{user.name}</strong><span>{user.email || user.faculty}</span></div>
      <div id="account-menu-panel" role="menu" aria-label="Действия пользователя" onKeyDown={event => {
        if (!["ArrowDown", "ArrowUp", "Home", "End"].includes(event.key)) return;
        event.preventDefault();
        const items = Array.from(panel.current?.querySelectorAll<HTMLButtonElement>("button") || []);
        const index = items.indexOf(document.activeElement as HTMLButtonElement);
        const next = event.key === "Home" ? 0 : event.key === "End" ? items.length - 1 : (index + (event.key === "ArrowDown" ? 1 : -1) + items.length) % items.length;
        items[next]?.focus();
      }}>
        <button role="menuitem" onClick={() => visit("/profile")}><UserRound size={17}/>Мой профиль</button>
        <button role="menuitem" onClick={() => visit("/my")}><FileText size={17}/>Мои материалы</button>
        <button role="menuitem" onClick={() => visit("/saved")}><Bookmark size={17}/>Избранное</button>
        <div className="account-separator" role="separator"/>
        <button role="menuitemcheckbox" aria-checked={theme === "dark"} onClick={toggleTheme}>
          {theme === "dark" ? <Moon size={17}/> : <Sun size={17}/>}Тёмная тема
          <span className={`theme-switch ${theme === "dark" ? "on" : ""}`} aria-hidden="true"><span/></span>
        </button>
        <div className="account-separator" role="separator"/>
        <button role="menuitem" className="account-logout" onClick={() => { setOpen(false); logout(); }}><LogOut size={17}/>Выйти</button>
      </div>
    </div>}
  </div>;
}
