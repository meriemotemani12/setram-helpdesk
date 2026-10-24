// Interface du helpdesk SETRAM : application à page unique, sans dépendance.
// Règles : aucun innerHTML (les saisies des utilisateurs ne sont jamais interprétées comme du HTML),
// aucun style ou script en ligne (la CSP du serveur les interdit).
(function () {
  "use strict";

  // ---------- Stockage (peut échouer en navigation privée : toujours protégé) ----------
  const store = {
    get(area, key) { try { return window[area].getItem(key); } catch (_) { return null; } },
    set(area, key, v) { try { window[area].setItem(key, v); } catch (_) { /* ignoré */ } },
    del(area, key) { try { window[area].removeItem(key); } catch (_) { /* ignoré */ } },
  };

  const state = {
    lang: store.get("localStorage", "lang") === "fr" ? "fr" : "ar",
    token: store.get("sessionStorage", "token"),
    user: null,
    staffUsers: null, // cache de la liste des utilisateurs (réservée au personnel)
  };
  const T = () => window.I18N[state.lang];
  const isStaff = () => state.user && (state.user.role === "technician" || state.user.role === "admin");
  const isAdmin = () => state.user && state.user.role === "admin";

  // ---------- Aides DOM ----------
  function h(tag, attrs, ...kids) {
    const el = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (v === false || v == null) continue;
      if (k === "class") el.className = v;
      else if (k.startsWith("on") && typeof v === "function") el.addEventListener(k.slice(2), v);
      else if (v === true) el.setAttribute(k, "");
      else el.setAttribute(k, String(v));
    }
    append(el, kids);
    return el;
  }
  function append(el, kids) {
    for (const k of kids.flat(Infinity)) {
      if (k == null || k === false) continue;
      el.append(k instanceof Node ? k : document.createTextNode(String(k)));
    }
  }
  const $ = (id) => document.getElementById(id);
  function mount(...nodes) {
    const main = $("main");
    main.replaceChildren();
    append(main, nodes);
  }
  let toastTimer;
  function toast(msg, isError) {
    const el = $("toast");
    el.textContent = msg;
    el.className = "toast" + (isError ? " error" : "");
    el.setAttribute("role", isError ? "alert" : "status");
    el.hidden = false;
    clearTimeout(toastTimer);
    toastTimer = setTimeout(() => { el.hidden = true; }, 4500);
  }

  // ---------- Icônes (SVG en ligne construits par le DOM : compatible avec la CSP) ----------
  // Tracés de type « contour » 24×24. Une chaîne = <path d>, un tableau = [balise, attributs].
  const SVG_NS = "http://www.w3.org/2000/svg";
  const ICONS = {
    dashboard: [["rect", { x: 3, y: 3, width: 7, height: 9, rx: 1 }], ["rect", { x: 14, y: 3, width: 7, height: 5, rx: 1 }],
      ["rect", { x: 14, y: 12, width: 7, height: 9, rx: 1 }], ["rect", { x: 3, y: 16, width: 7, height: 5, rx: 1 }]],
    ticket: ["M2 9a3 3 0 0 1 0 6v2a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-2a3 3 0 0 1 0-6V7a2 2 0 0 0-2-2H4a2 2 0 0 0-2 2Z", "M13 5v2", "M13 17v2", "M13 11v2"],
    plusCircle: [["circle", { cx: 12, cy: 12, r: 10 }], "M8 12h8", "M12 8v8"],
    plus: ["M5 12h14", "M12 5v14"],
    monitor: [["rect", { x: 2, y: 3, width: 20, height: 14, rx: 2 }], "M8 21h8", "M12 17v4"],
    users: ["M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2", ["circle", { cx: 9, cy: 7, r: 4 }], "M22 21v-2a4 4 0 0 0-3-3.87", "M16 3.13a4 4 0 0 1 0 7.75"],
    globe: [["circle", { cx: 12, cy: 12, r: 10 }], "M2 12h20", "M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"],
    logout: ["M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4", "M16 17l5-5-5-5", "M21 12H9"],
    menu: ["M4 6h16", "M4 12h16", "M4 18h16"],
    x: ["M18 6 6 18", "M6 6l12 12"],
    refresh: ["M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8", "M21 3v5h-5", "M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16", "M8 16H3v5"],
    search: [["circle", { cx: 11, cy: 11, r: 8 }], "m21 21-4.3-4.3"],
    clock: [["circle", { cx: 12, cy: 12, r: 10 }], "M12 6v6l4 2"],
    timer: [["circle", { cx: 12, cy: 14, r: 8 }], "M12 10v4l2 2", "M9 2h6"],
    check: ["M22 11.08V12a10 10 0 1 1-5.93-9.14", "m9 11 3 3L22 4"],
    alert: ["m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z", "M12 9v4", "M12 17h.01"],
    progress: ["M21 12a9 9 0 1 1-6.219-8.56"],
    inbox: ["M22 12h-6l-2 3h-4l-2-3H2", "M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"],
    arrow: ["M5 12h14", "m12 5 7 7-7 7"],
    sparkles: ["m12 3-1.9 5.8a2 2 0 0 1-1.3 1.3L3 12l5.8 1.9a2 2 0 0 1 1.3 1.3L12 21l1.9-5.8a2 2 0 0 1 1.3-1.3L21 12l-5.8-1.9a2 2 0 0 1-1.3-1.3Z"],
    calendar: [["rect", { x: 3, y: 4, width: 18, height: 18, rx: 2 }], "M16 2v4", "M8 2v4", "M3 10h18"],
    wrench: ["M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"],
    eye: ["M2 12s3-7 10-7 10 7 10 7-3 7-10 7-10-7-10-7Z", ["circle", { cx: 12, cy: 12, r: 3 }]],
    pencil: ["M17 3a2.85 2.85 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z"],
    chevron: ["m9 18 6-6-6-6"],
  };
  function icon(name, extraClass) {
    const svg = document.createElementNS(SVG_NS, "svg");
    svg.setAttribute("viewBox", "0 0 24 24");
    svg.setAttribute("fill", "none");
    svg.setAttribute("stroke", "currentColor");
    svg.setAttribute("stroke-width", "2");
    svg.setAttribute("stroke-linecap", "round");
    svg.setAttribute("stroke-linejoin", "round");
    svg.setAttribute("aria-hidden", "true");
    svg.setAttribute("focusable", "false");
    svg.setAttribute("class", "icon" + (extraClass ? " " + extraClass : ""));
    for (const part of ICONS[name] || []) {
      const [tagName, attrs] = typeof part === "string" ? ["path", { d: part }] : part;
      const el = document.createElementNS(SVG_NS, tagName);
      for (const [k, v] of Object.entries(attrs)) el.setAttribute(k, String(v));
      svg.append(el);
    }
    return svg;
  }
  // Remplace les emplacements <span data-icon="…"> du HTML statique par les icônes.
  function hydrateIcons() {
    document.querySelectorAll("[data-icon]").forEach((el) => el.replaceWith(icon(el.getAttribute("data-icon"))));
  }

  // ---------- Formats ----------
  function fmtDate(iso, withTime) {
    if (!iso) return T().ticket.none;
    const d = new Date(iso);
    if (isNaN(d)) return T().ticket.none;
    return new Intl.DateTimeFormat(T().locale, withTime
      ? { dateStyle: "medium", timeStyle: "short" } : { dateStyle: "medium" }).format(d);
  }
  function fmtHours(hours) {
    const d = T().dash;
    if (!hours || hours <= 0) return "0 " + d.minutes;
    const mins = Math.round(hours * 60);
    if (mins < 60) return mins + " " + d.minutes;
    if (mins < 2880) {
      const hh = Math.floor(mins / 60), mm = mins % 60;
      return hh + " " + d.hours + (mm ? " " + mm + " " + d.minutes : "");
    }
    return Math.round(mins / 1440) + " " + d.days;
  }
  const dateInput = (iso) => (iso ? String(iso).slice(0, 10) : "");
  const dateToISO = (v) => (v ? v + "T00:00:00Z" : null);
  function isOverdue(e) {
    return e.status === "active" && e.next_maintenance_at && new Date(e.next_maintenance_at) < new Date();
  }
  const tag = (cls, text) => h("span", { class: "tag " + cls }, text);

  // ---------- Appels API ----------
  class ApiError extends Error {
    constructor(status, body) { super((body && body.error) || "error"); this.status = status; this.field = body && body.field; }
  }
  async function api(path, opts = {}) {
    const headers = { Accept: "application/json" };
    if (opts.body !== undefined) headers["Content-Type"] = "application/json";
    if (state.token) headers.Authorization = "Bearer " + state.token;
    let res;
    try {
      res = await fetch("/api" + path, {
        method: opts.method || "GET", headers,
        body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
      });
    } catch (_) {
      throw new ApiError(0, { error: T().errors.network });
    }
    let body = null;
    try { body = await res.json(); } catch (_) { /* corps vide */ }
    if (!res.ok) {
      if (res.status === 401 && state.token && path !== "/auth/login") {
        logout(true);
      }
      throw new ApiError(res.status, body);
    }
    return body;
  }
  function errText(err) {
    const e = T().errors;
    if (!(err instanceof ApiError)) return e.generic;
    switch (err.status) {
      case 0: return e.network;
      case 401: return e.session;
      case 403: return e.forbidden;
      case 404: return e.notFound;
      case 409: return /transition|statut/i.test(err.message) ? e.transition : e.conflict;
      case 422: return e.fix;
      case 429: return T().login.tooMany;
      default: return e.generic;
    }
  }
  // Message d'un champ invalide : libellé traduit + message du serveur (en français) quand l'interface est en français.
  function fieldMessage(err) {
    const label = T().fields[err.field] || err.field;
    return state.lang === "fr" ? label + " : " + err.message : T().errors.fix + " (" + label + ")";
  }
  function showFormError(form, err) {
    form.querySelectorAll(".field-error, .form-error").forEach((n) => n.remove());
    form.querySelectorAll("[aria-invalid]").forEach((n) => n.removeAttribute("aria-invalid"));
    const input = err.field && form.elements[err.field];
    if (err instanceof ApiError && err.status === 422 && input && input.setAttribute) {
      input.setAttribute("aria-invalid", "true");
      const id = "err-" + err.field;
      input.setAttribute("aria-describedby", id);
      input.closest("label").append(h("span", { class: "field-error", id }, fieldMessage(err)));
      input.focus();
      return;
    }
    const box = h("p", { class: "form-error", role: "alert" }, err instanceof ApiError && err.status === 422 ? fieldMessage(err) : errText(err));
    form.prepend(box);
  }
  // Bouton en cours d'envoi : désactivé, animé et annoncé aux lecteurs d'écran.
  function setBusy(btn, busy) {
    btn.disabled = busy;
    btn.classList.toggle("loading", busy);
    if (busy) btn.setAttribute("aria-busy", "true"); else btn.removeAttribute("aria-busy");
  }
  async function submitForm(form, btn, fn) {
    setBusy(btn, true);
    try { await fn(); } catch (err) { showFormError(form, err); } finally { setBusy(btn, false); }
  }

  // ---------- Champs de formulaire ----------
  function field(label, control, hint) {
    return h("label", {}, label, hint ? h("span", { class: "hint" }, hint) : null, control);
  }
  function select(name, options, value, opts = {}) {
    const el = h("select", { name, id: opts.id, required: opts.required });
    for (const [v, text] of options) {
      const o = h("option", { value: v }, text);
      if (String(v) === String(value == null ? "" : value)) o.selected = true;
      el.append(o);
    }
    return el;
  }
  const enumOptions = (map, keys) => keys.map((k) => [k, map[k]]);
  const CATEGORIES = ["computer", "network", "printer", "ticketing_counter", "other"];
  const PRIORITIES = ["low", "medium", "high", "critical"];
  const STATUSES = ["new", "in_progress", "done"];
  const EQUIP_TYPES = ["computer", "network", "printer", "ticket_machine", "other"];
  const EQUIP_STATUSES = ["active", "in_repair", "retired"];

  async function getStaffUsers() {
    if (!isStaff()) return [];
    if (!state.staffUsers) state.staffUsers = await api("/users");
    return state.staffUsers;
  }

  // ---------- Session ----------
  function logout(expired) {
    state.token = null; state.user = null; state.staffUsers = null;
    store.del("sessionStorage", "token");
    renderBand();
    location.hash = "#/login";
    if (expired) toast(T().errors.session, true);
  }

  function applyLang() {
    const t = T();
    document.documentElement.lang = state.lang;
    document.documentElement.dir = t.dir;
    document.title = t.appName + " · " + t.appSub;
    $("skip").textContent = t.skip;
    $("brand-name").textContent = t.appName;
    $("brand-sub").textContent = t.appSub;
    $("topbar-name").textContent = t.appName;
    $("lang-label").textContent = t.other;
    $("logout-label").textContent = t.logout;
    $("nav").setAttribute("aria-label", t.appSub);
    $("menu-toggle").setAttribute("aria-label", t.menu);
    $("menu-close").setAttribute("aria-label", t.close);
  }

  // ---------- Tiroir de navigation (mobile) ----------
  const desktopQuery = window.matchMedia("(min-width: 900px)");
  function setMenu(open, returnFocus) {
    const band = $("band");
    band.classList.toggle("open", open);
    $("backdrop").hidden = !open;
    $("menu-toggle").setAttribute("aria-expanded", String(open));
    document.body.classList.toggle("menu-open", open);
    if (open) $("menu-close").focus();
    else if (returnFocus) $("menu-toggle").focus();
  }
  $("menu-toggle").addEventListener("click", () => setMenu(!$("band").classList.contains("open")));
  $("menu-close").addEventListener("click", () => setMenu(false, true));
  $("backdrop").addEventListener("click", () => setMenu(false, true));
  document.addEventListener("keydown", (ev) => {
    if (ev.key === "Escape" && $("band").classList.contains("open")) setMenu(false, true);
  });
  const onViewport = () => { if (desktopQuery.matches) setMenu(false); };
  if (desktopQuery.addEventListener) desktopQuery.addEventListener("change", onViewport);

  const initials = (name) => Array.from((name || "?").trim().split(/\s+/).slice(0, 2).map((w) => Array.from(w)[0] || "")).join("").toUpperCase();

  // Barre latérale : visible seulement quand un utilisateur est connecté.
  function renderBand() {
    applyLang();
    const authed = !!state.user;
    $("band").hidden = !authed;
    $("topbar").hidden = !authed;
    document.body.classList.toggle("has-shell", authed);
    const nav = $("nav");
    nav.replaceChildren();
    if (!authed) {
      setMenu(false);
      $("who-name").textContent = ""; $("who-role").textContent = ""; $("avatar").textContent = "";
      return;
    }
    const t = T();
    $("who-name").textContent = state.user.full_name;
    $("who-role").textContent = t.role[state.user.role];
    $("avatar").textContent = initials(state.user.full_name);
    const items = [];
    if (isStaff()) items.push(["#/dashboard", t.nav.dashboard, "dashboard"]);
    items.push(["#/tickets", t.nav.tickets, "ticket"], ["#/new", t.nav.new, "plusCircle"], ["#/equipment", t.nav.equipment, "monitor"]);
    if (isAdmin()) items.push(["#/users", t.nav.users, "users"]);
    const section = (location.hash || "#/tickets").slice(2).split("?")[0].split("/")[0];
    for (const [href, text, ic] of items) {
      const a = h("a", { href }, icon(ic), h("span", {}, text));
      if (href.slice(2) === section) a.setAttribute("aria-current", "page");
      nav.append(a);
    }
  }

  // ---------- Composants de page ----------
  function pageHead(title, subtitle, actions) {
    return h("div", { class: "page-head" }, h("div", {}, h("h1", {}, title), subtitle ? h("p", {}, subtitle) : null), actions || null);
  }
  const loading = () => mount(h("div", { class: "loading", role: "status" }, h("span", { class: "spinner", "aria-hidden": "true" }), T().loading));
  function failPage(err) {
    mount(h("div", { class: "panel state error", role: "alert" }, icon("alert"), h("p", {}, errText(err)),
      h("button", { type: "button", class: "btn secondary", onclick: () => route() }, icon("refresh"), T().retry)));
  }
  function emptyState(title, body, compact) {
    return h("div", { class: "empty" + (compact ? " compact" : "") }, compact ? null : icon("inbox"), title ? h("strong", {}, title) : null, body);
  }
  // Cellule de tableau avec son libellé (affiché sur mobile quand le tableau devient une liste de cartes).
  const td = (label, cls, ...kids) => h("td", { "data-label": label, class: cls || false }, kids);
  const tableOf = (heads, rows) => h("div", { class: "table-wrap" }, h("table", { class: "rtable" },
    h("thead", {}, h("tr", {}, heads.map((x) => h("th", { scope: "col" }, x)))), h("tbody", {}, rows)));
  function panel(title, body, opts = {}) {
    return h("section", { class: "panel" + (opts.cls ? " " + opts.cls : "") },
      h("div", { class: "panel-head" }, h("h2", {}, title), opts.action || null), body);
  }
  function pageHref(base, params, page) {
    const q = new URLSearchParams(params);
    if (page > 1) q.set("page", String(page)); else q.delete("page");
    const s = q.toString();
    return base + (s ? "?" + s : "");
  }
  // Pagination côté client (l'API renvoie la liste complète).
  function pager(base, params, page, pages, from, to, total) {
    if (total <= 0 || pages <= 1) return null;
    const p = T().pagination;
    // Le chevron pointe vers la droite par défaut : « suivant » le retourne en arabe, « précédent » l'inverse.
    const step = (label, target, disabled, isNext) => {
      const ic = icon("chevron", isNext ? "flip" : "flip-back");
      const kids = isNext ? [label, ic] : [ic, label];
      return disabled
        ? h("span", { class: "btn secondary sm", "aria-disabled": "true" }, kids)
        : h("a", { class: "btn secondary sm", href: pageHref(base, params, target) }, kids);
    };
    return h("nav", { class: "pager", "aria-label": p.label },
      h("span", {}, from + "–" + to + " " + p.of + " " + total),
      h("div", { class: "btn-row" },
        step(p.prev, page - 1, page <= 1, false),
        h("span", {}, page + " / " + pages),
        step(p.next, page + 1, page >= pages, true)));
  }

  // --- Connexion ---
  function pageLogin() {
    renderBand();
    const t = T();
    const form = h("form", { class: "stack", novalidate: true });
    const email = h("input", { type: "email", name: "email", autocomplete: "username", required: true, dir: "ltr" });
    const pass = h("input", { type: "password", name: "password", autocomplete: "current-password", required: true, dir: "ltr" });
    const btn = h("button", { class: "btn", type: "submit" }, t.login.submit);
    form.append(field(t.login.email, email), field(t.login.password, pass), btn);
    form.addEventListener("submit", async (ev) => {
      ev.preventDefault();
      form.querySelectorAll(".form-error").forEach((n) => n.remove());
      setBusy(btn, true);
      try {
        const res = await api("/auth/login", { method: "POST", body: { email: email.value, password: pass.value } });
        state.token = res.token; state.user = res.user;
        store.set("sessionStorage", "token", res.token);
        location.hash = "#/tickets";
        route();
      } catch (err) {
        const msg = err.status === 401 ? t.login.failed : errText(err);
        form.prepend(h("p", { class: "form-error", role: "alert" }, msg));
        pass.value = ""; pass.focus();
      } finally { setBusy(btn, false); }
    });
    const langBtn = h("button", { type: "button", class: "btn secondary sm", onclick: toggleLang }, icon("globe"), t.other);
    mount(h("div", { class: "login" }, h("div", { class: "panel" },
      h("div", { class: "logo" }, h("img", { src: "/assets/logo.svg", alt: "", width: 56, height: 56 }),
        h("div", {}, h("strong", {}, t.appName), h("br"), t.appSub)),
      h("h1", {}, t.login.title), form, h("p", {}, langBtn))));
    email.focus();
  }

  // --- Liste des demandes ---
  const PAGE_SIZE = 10;
  async function pageTickets() {
    const t = T(), tt = t.tickets;
    loading();
    const params = new URLSearchParams((location.hash.split("?")[1]) || "");
    // « priority » et « page » sont gérés côté client : on ne les envoie pas à l'API.
    const apiParams = new URLSearchParams(params);
    apiParams.delete("priority"); apiParams.delete("page");
    const prioFilter = params.get("priority") || "";
    const q = h("input", { type: "search", name: "q", value: params.get("q") || "", placeholder: tt.search + " — " + tt.searchHint, "aria-label": tt.search });
    const st = select("status", [["", tt.allStatuses], ...enumOptions(t.status, STATUSES)], params.get("status"));
    const pr = select("priority", [["", tt.allPriorities], ...enumOptions(t.priority, PRIORITIES)], prioFilter);
    const cat = select("category", [["", tt.allCategories], ...enumOptions(t.category, CATEGORIES)], params.get("category"));
    st.setAttribute("aria-label", tt.cols.status); pr.setAttribute("aria-label", tt.cols.priority); cat.setAttribute("aria-label", tt.cols.category);
    const mine = h("input", { type: "checkbox", name: "assignee", value: "me" });
    mine.checked = params.get("assignee") === "me";
    const hasFilter = [...params.keys()].some((k) => k !== "page");
    const filter = h("form", { class: "panel toolbar", role: "search" },
      h("label", { class: "search" }, h("span", { class: "sr-only" }, tt.search), icon("search"), q),
      st, pr, cat,
      isStaff() ? h("label", { class: "checkline" }, mine, tt.assignedToMe) : null,
      h("button", { class: "btn", type: "submit" }, tt.apply),
      hasFilter ? h("a", { class: "btn secondary", href: "#/tickets" }, tt.reset) : null);
    filter.addEventListener("submit", (ev) => {
      ev.preventDefault();
      const p = new URLSearchParams();
      if (q.value.trim()) p.set("q", q.value.trim());
      if (st.value) p.set("status", st.value);
      if (pr.value) p.set("priority", pr.value);
      if (cat.value) p.set("category", cat.value);
      if (mine.checked) p.set("assignee", "me");
      // Un filtre « équipement » venant de la fiche matériel est conservé.
      if (params.get("equipment_id")) p.set("equipment_id", params.get("equipment_id"));
      const s = p.toString();
      location.hash = "#/tickets" + (s ? "?" + s : "");
    });

    let list;
    try { list = await api("/tickets" + (apiParams.toString() ? "?" + apiParams : "")); } catch (e) { return failPage(e); }
    if (prioFilter) list = list.filter((x) => x.priority === prioFilter);
    const total = list.length;
    const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));
    const page = Math.min(Math.max(1, parseInt(params.get("page") || "1", 10) || 1), pages);
    const slice = list.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);

    const head = pageHead(isStaff() ? tt.title : tt.mine, tt.subtitle,
      h("div", { class: "page-actions" }, h("a", { class: "btn", href: "#/new" }, icon("plus"), t.nav.new)));
    let body;
    if (!total) {
      body = h("div", { class: "table-wrap" }, emptyState(tt.emptyTitle, tt.emptyBody));
    } else {
      const c = tt.cols;
      body = tableOf([c.id, c.title, c.category, c.priority, c.status, c.requester, c.assignee, c.created, ""],
        slice.map((x) => h("tr", {},
          td(c.id, "num", "#" + x.id),
          td(c.title, null, h("a", { class: "row-link", href: "#/tickets/" + x.id }, x.title)),
          td(c.category, null, t.category[x.category]),
          td(c.priority, null, tag(x.priority, t.priority[x.priority])),
          td(c.status, null, tag(x.status, t.status[x.status])),
          td(c.requester, null, x.requester_name || ""),
          td(c.assignee, null, x.assignee_name || t.ticket.none),
          td(c.created, "num", fmtDate(x.created_at)),
          td("", "actions", h("a", { class: "btn secondary sm", href: "#/tickets/" + x.id, "aria-label": t.view + " #" + x.id }, icon("eye"), t.view)))));
    }
    const from = total ? (page - 1) * PAGE_SIZE + 1 : 0, to = Math.min(page * PAGE_SIZE, total);
    mount(head, filter, body, pager("#/tickets", params, page, pages, from, to, total));
  }

  // --- Nouvelle demande ---
  async function pageNewTicket() {
    const t = T(), n = t.newTicket;
    loading();
    let equipment;
    try { equipment = await api("/equipment"); } catch (e) { return failPage(e); }
    const form = h("form", { class: "stack", novalidate: true });
    const title = h("input", { name: "title", maxlength: 150, required: true });
    const desc = h("textarea", { name: "description", maxlength: 5000, required: true });
    const cat = select("category", [["", n.auto], ...enumOptions(t.category, CATEGORIES)], "");
    const pri = select("priority", [["", n.auto], ...enumOptions(t.priority, PRIORITIES)], "");
    const eq = select("equipment_id", [["", n.noEquipment], ...equipment.filter((e) => e.status !== "retired")
      .map((e) => [e.id, e.asset_tag + " · " + e.name + (e.location ? " (" + e.location + ")" : "")])], "");
    const sugBox = h("div", { "aria-live": "polite" });
    const suggestBtn = h("button", { type: "button", class: "btn secondary" }, icon("sparkles"), n.suggest);
    const submit = h("button", { type: "submit", class: "btn" }, n.submit);

    suggestBtn.addEventListener("click", async () => {
      sugBox.replaceChildren();
      if (desc.value.trim().length < 3) { desc.focus(); return; }
      setBusy(suggestBtn, true);
      try {
        const s = await api("/tickets/classify", { method: "POST", body: { title: title.value, description: desc.value } });
        const apply = h("button", { type: "button", class: "btn secondary sm" }, n.applySuggestion);
        apply.addEventListener("click", () => { cat.value = s.category; pri.value = s.priority; });
        sugBox.append(h("div", { class: "suggestion" },
          h("p", {}, h("strong", {}, n.suggestionTitle + " (" + (n.suggestionSource[s.source] || s.source) + ")")),
          h("p", {}, t.ticket.aiCategory + " : " + t.category[s.category]),
          h("p", {}, t.ticket.aiPriority + " : " + t.priority[s.priority]),
          s.summary ? h("p", { lang: "fr" }, s.summary) : null, apply));
      } catch (err) { showFormError(form, err); }
      finally { setBusy(suggestBtn, false); }
    });

    form.append(field(n.ttl, title), field(n.description, desc, n.descriptionHint),
      h("div", { class: "btn-row" }, suggestBtn), sugBox,
      h("div", { class: "row" }, field(n.category, cat), field(n.priority, pri)),
      field(n.equipment, eq),
      h("div", { class: "btn-row" }, submit, h("a", { class: "btn secondary", href: "#/tickets" }, t.cancel)));
    form.addEventListener("submit", (ev) => {
      ev.preventDefault();
      submitForm(form, submit, async () => {
        const body = { title: title.value, description: desc.value, category: cat.value, priority: pri.value,
          equipment_id: eq.value ? Number(eq.value) : null };
        const created = await api("/tickets", { method: "POST", body });
        toast(n.created);
        location.hash = "#/tickets/" + created.id;
      });
    });
    mount(pageHead(n.title, n.subtitle), h("div", { class: "panel form-panel" }, form));
    title.focus();
  }

  // --- Détail d'une demande ---
  async function pageTicket(id) {
    const t = T(), d = t.ticket;
    loading();
    let detail, users = [], equipment = [];
    try {
      detail = await api("/tickets/" + id);
      if (isStaff()) [users, equipment] = await Promise.all([getStaffUsers(), api("/equipment")]);
    } catch (e) { return failPage(e); }
    const tk = detail.ticket;
    const render = () => pageTicket(id);

    // Ligne de tramway : trois arrêts (nouveau → en cours → terminé)
    const idx = STATUSES.indexOf(tk.status);
    const line = h("ol", { class: "line", "aria-label": d.progress }, STATUSES.map((s, i) => h("li", {
      class: "stop" + (i < idx ? " passed" : i === idx ? " current" : ""), "aria-current": i === idx ? "step" : false,
    }, h("strong", {}, t.status[s]),
      h("span", {}, s === "new" ? fmtDate(tk.created_at, true) : s === "in_progress" ? (tk.started_at ? fmtDate(tk.started_at, true) : "") : (tk.resolved_at ? fmtDate(tk.resolved_at, true) : "")))));

    const dur = tk.resolved_at ? fmtHours((new Date(tk.resolved_at) - new Date(tk.created_at)) / 36e5) : null;
    const meta = h("dl", { class: "meta" },
      metaItem(t.tickets.cols.category, t.category[tk.category]),
      metaItem(t.tickets.cols.priority, tag(tk.priority, t.priority[tk.priority])),
      metaItem(d.requester, tk.requester_name || d.none),
      metaItem(d.assignee, tk.assignee_name || d.unassigned),
      metaItem(d.equipment, tk.equipment_tag || d.none),
      metaItem(d.created, fmtDate(tk.created_at, true)),
      dur ? metaItem(d.duration, dur) : null);

    const ai = tk.ai_category ? h("div", { class: "suggestion" }, h("p", {}, h("strong", {}, d.aiTitle)),
      h("p", {}, d.aiCategory + " : " + t.category[tk.ai_category]),
      tk.ai_priority ? h("p", {}, d.aiPriority + " : " + t.priority[tk.ai_priority]) : null,
      tk.ai_summary ? h("p", { lang: "fr" }, tk.ai_summary) : null) : null;

    // Actions de workflow (personnel uniquement)
    let actions = null;
    if (isStaff()) {
      const go = (to) => async () => {
        try { await api("/tickets/" + id + "/status", { method: "POST", body: { status: to } }); toast(d.statusChanged); render(); }
        catch (err) { toast(errText(err), true); }
      };
      const btns = [];
      if (tk.status === "new") btns.push(h("button", { class: "btn", type: "button", onclick: go("in_progress") }, d.take));
      if (tk.status === "in_progress") {
        btns.push(h("button", { class: "btn", type: "button", onclick: go("done") }, d.finish));
        btns.push(h("button", { class: "btn secondary", type: "button", onclick: go("new") }, d.release));
      }
      if (tk.status === "done") btns.push(h("button", { class: "btn secondary", type: "button", onclick: go("in_progress") }, d.reopen));

      const form = h("form", { class: "stack", novalidate: true });
      const pri = select("priority", enumOptions(t.priority, PRIORITIES), tk.priority);
      const cat = select("category", enumOptions(t.category, CATEGORIES), tk.category);
      const asg = select("assignee_id", [[0, d.unassigned], ...users.filter((u) => u.active).map((u) => [u.id, u.full_name])], tk.assignee_id || 0);
      const eq = select("equipment_id", [[0, d.none], ...equipment.map((e) => [e.id, e.asset_tag + " · " + e.name])], tk.equipment_id || 0);
      const save = h("button", { class: "btn secondary", type: "submit" }, d.save);
      form.append(h("div", { class: "row" }, field(t.tickets.cols.priority, pri), field(t.tickets.cols.category, cat)),
        h("div", { class: "row" }, field(d.assign, asg), field(d.equipment, eq)), h("div", { class: "btn-row" }, save));
      form.addEventListener("submit", (ev) => {
        ev.preventDefault();
        submitForm(form, save, async () => {
          await api("/tickets/" + id, { method: "PATCH", body: {
            priority: pri.value, category: cat.value, assignee_id: Number(asg.value), equipment_id: Number(eq.value) } });
          toast(d.saved); render();
        });
      });
      actions = h("section", { class: "panel" }, h("h2", {}, d.actions), h("div", { class: "btn-row" }, btns), h("hr"), form);
    }

    // Commentaires
    const cBody = h("textarea", { name: "body", maxlength: 2000, required: true });
    const cSend = h("button", { class: "btn", type: "submit" }, d.send);
    const cForm = h("form", { class: "stack", novalidate: true }, field(d.addComment, cBody), h("div", { class: "btn-row" }, cSend));
    cForm.addEventListener("submit", (ev) => {
      ev.preventDefault();
      submitForm(cForm, cSend, async () => {
        await api("/tickets/" + id + "/comments", { method: "POST", body: { body: cBody.value } });
        toast(d.commentAdded); render();
      });
    });
    const thread = detail.comments && detail.comments.length
      ? h("ul", { class: "thread" }, detail.comments.map((c) => h("li", {},
        h("bdi", { class: "by" }, c.author_name || ""), h("time", { datetime: c.created_at }, fmtDate(c.created_at, true)), h("p", {}, c.body))))
      : h("p", {}, d.noComments);

    // Le serveur enregistre aussi la création (from_status = null) : on s'appuie uniquement sur son historique.
    const history = h("ol", { class: "history" }, (detail.history || []).map((c) => h("li", {},
      (c.from_status ? d.from + " " + t.status[c.from_status] + " " + d.to + " " + t.status[c.to_status] : d.opened + " (" + t.status[c.to_status] + ")")
        + (c.changer_name ? " · " + d.by + " " : ""),
      c.changer_name ? h("bdi", {}, c.changer_name) : null,
      h("time", { datetime: c.changed_at }, fmtDate(c.changed_at, true)))));

    mount(
      h("a", { class: "back-link", href: "#/tickets" }, icon("arrow", "flip-back"), d.back),
      pageHead("#" + tk.id + " · " + tk.title, null,
        h("div", { class: "page-actions" }, tag(tk.priority, t.priority[tk.priority]), tag(tk.status, t.status[tk.status]))),
      line,
      h("div", { class: "cols two" },
        h("div", {},
          h("section", { class: "panel" }, h("h2", {}, d.details), meta, ai,
            h("h3", {}, d.description), h("p", { class: "desc" }, tk.description)),
          h("section", { class: "panel" }, h("h2", {}, d.comments), thread, cForm)),
        h("div", {}, actions, h("section", { class: "panel" }, h("h2", {}, d.history), history))));
  }
  function metaItem(label, value) { return h("div", {}, h("dt", {}, label), h("dd", {}, value)); }

  // --- Inventaire ---
  async function pageEquipment() {
    const t = T(), e = t.equipment;
    loading();
    const params = new URLSearchParams((location.hash.split("?")[1]) || "");
    const apiParams = new URLSearchParams(params);
    apiParams.delete("page");
    const q = h("input", { type: "search", name: "q", value: params.get("q") || "", placeholder: e.search + " — " + e.searchHint, "aria-label": e.search });
    const ty = select("type", [["", e.allTypes], ...enumOptions(t.equipType, EQUIP_TYPES)], params.get("type"));
    const st = select("status", [["", e.allStatuses], ...enumOptions(t.equipStatus, EQUIP_STATUSES)], params.get("status"));
    ty.setAttribute("aria-label", e.cols.type); st.setAttribute("aria-label", e.cols.status);
    const hasFilter = [...params.keys()].some((k) => k !== "page");
    const filter = h("form", { class: "panel toolbar", role: "search" },
      h("label", { class: "search" }, h("span", { class: "sr-only" }, e.search), icon("search"), q), ty, st,
      h("button", { class: "btn", type: "submit" }, t.tickets.apply),
      hasFilter ? h("a", { class: "btn secondary", href: "#/equipment" }, t.tickets.reset) : null);
    filter.addEventListener("submit", (ev) => {
      ev.preventDefault();
      const p = new URLSearchParams();
      if (q.value.trim()) p.set("q", q.value.trim());
      if (ty.value) p.set("type", ty.value);
      if (st.value) p.set("status", st.value);
      const s = p.toString();
      location.hash = "#/equipment" + (s ? "?" + s : "");
    });
    let list;
    try { list = await api("/equipment" + (apiParams.toString() ? "?" + apiParams : "")); } catch (err) { return failPage(err); }
    const total = list.length;
    const pages = Math.max(1, Math.ceil(total / PAGE_SIZE));
    const page = Math.min(Math.max(1, parseInt(params.get("page") || "1", 10) || 1), pages);
    const slice = list.slice((page - 1) * PAGE_SIZE, page * PAGE_SIZE);
    let body;
    if (!total) {
      body = h("div", { class: "table-wrap" }, emptyState(e.emptyTitle, e.emptyBody));
    } else {
      const c = e.cols;
      body = tableOf([c.tag, c.name, c.type, c.location, c.responsible, c.status, c.last, c.next, ...(isStaff() ? [""] : [])],
        slice.map((x) => h("tr", {},
          td(c.tag, "num", isStaff() ? h("a", { class: "row-link", href: "#/equipment/" + x.id }, x.asset_tag) : x.asset_tag),
          td(c.name, null, x.name, x.notes ? h("small", {}, x.notes) : null),
          td(c.type, null, t.equipType[x.type]), td(c.location, null, x.location),
          td(c.responsible, null, x.responsible_name || t.ticket.none),
          td(c.status, null, tag(x.status, t.equipStatus[x.status])),
          td(c.last, "num", fmtDate(x.last_maintenance_at)),
          td(c.next, "num", fmtDate(x.next_maintenance_at), " ", isOverdue(x) ? tag("overdue", e.overdue) : null),
          isStaff() ? td("", "actions", h("a", { class: "btn secondary sm", href: "#/equipment/" + x.id, "aria-label": t.edit + " " + x.asset_tag }, icon("pencil"), t.edit)) : null)));
    }
    const from = total ? (page - 1) * PAGE_SIZE + 1 : 0, to = Math.min(page * PAGE_SIZE, total);
    mount(pageHead(e.title, e.subtitle, isStaff() ? h("div", { class: "page-actions" }, h("a", { class: "btn", href: "#/equipment/new" }, icon("plus"), e.add)) : null),
      filter, body, pager("#/equipment", params, page, pages, from, to, total));
  }

  async function pageEquipmentForm(id) {
    const t = T(), e = t.equipment;
    if (!isStaff()) { location.hash = "#/equipment"; return; }
    loading();
    let item = null, users;
    try {
      users = await getStaffUsers();
      if (id) item = await api("/equipment/" + id);
    } catch (err) { return failPage(err); }
    const form = h("form", { class: "stack", novalidate: true });
    const tagIn = h("input", { name: "asset_tag", maxlength: 50, required: true, value: item ? item.asset_tag : "" });
    const name = h("input", { name: "name", maxlength: 150, required: true, value: item ? item.name : "" });
    const type = select("type", enumOptions(t.equipType, EQUIP_TYPES), item ? item.type : "computer");
    const loc = h("input", { name: "location", maxlength: 150, value: item ? item.location : "" });
    const resp = select("responsible_id", [[0, e.noResponsible], ...users.filter((u) => u.active).map((u) => [u.id, u.full_name])], item && item.responsible_id || 0);
    const status = select("status", enumOptions(t.equipStatus, EQUIP_STATUSES), item ? item.status : "active");
    const last = h("input", { type: "date", name: "last_maintenance_at", value: item ? dateInput(item.last_maintenance_at) : "" });
    const next = h("input", { type: "date", name: "next_maintenance_at", value: item ? dateInput(item.next_maintenance_at) : "" });
    const notes = h("textarea", { name: "notes", maxlength: 1000 }, item ? item.notes : "");
    const save = h("button", { class: "btn", type: "submit" }, e.save);
    form.append(h("div", { class: "row" }, field(e.tag, tagIn), field(e.nameLabel, name)),
      h("div", { class: "row" }, field(e.type, type), field(e.statusLabel, status)),
      h("div", { class: "row" }, field(e.location, loc), field(e.responsible, resp)),
      h("div", { class: "row" }, field(e.lastLabel, last), field(e.nextLabel, next)),
      field(e.notes, notes),
      h("div", { class: "btn-row" }, save, h("a", { class: "btn secondary", href: "#/equipment" }, e.cancel)));
    form.addEventListener("submit", (ev) => {
      ev.preventDefault();
      submitForm(form, save, async () => {
        const body = { asset_tag: tagIn.value, name: name.value, type: type.value, location: loc.value,
          responsible_id: Number(resp.value) || null, status: status.value,
          last_maintenance_at: dateToISO(last.value), next_maintenance_at: dateToISO(next.value), notes: notes.value };
        await api(id ? "/equipment/" + id : "/equipment", { method: id ? "PUT" : "POST", body });
        toast(e.saved);
        location.hash = "#/equipment";
      });
    });
    const extra = item ? h("p", {}, h("a", { href: "#/tickets?equipment_id=" + item.id }, e.viewTickets)) : null;
    mount(pageHead(item ? e.edit : e.add), h("div", { class: "panel form-panel" }, form), extra);
  }

  // --- Tableau de bord ---
  // Histogramme horizontal en SVG (largeurs en attributs SVG : aucun style en ligne).
  function barChart(rows, emptyText) {
    const max = Math.max(...rows.map((r) => r.value), 0);
    if (!rows.length || max === 0) return emptyState(null, emptyText, true);
    return h("ul", { class: "bars" }, rows.map((r) => {
      const svg = document.createElementNS(SVG_NS, "svg");
      svg.setAttribute("viewBox", "0 0 100 14"); svg.setAttribute("preserveAspectRatio", "none"); svg.setAttribute("aria-hidden", "true");
      const mk = (cls, w) => {
        const rc = document.createElementNS(SVG_NS, "rect");
        rc.setAttribute("class", cls); rc.setAttribute("x", "0"); rc.setAttribute("y", "0"); rc.setAttribute("height", "14");
        rc.setAttribute("width", String(w)); rc.setAttribute("rx", "2");
        return rc;
      };
      svg.append(mk("track", 100), mk("fill" + (r.cls ? " " + r.cls : ""), r.value > 0 ? Math.max(2, (r.value / max) * 100) : 0));
      return h("li", {}, h("span", { class: "bar-label" }, r.label), svg, h("output", { "aria-label": r.label + " : " + r.value }, r.value));
    }));
  }
  // Anneau de répartition (cercle SVG de circonférence 100 : les longueurs de trait sont des pourcentages).
  function donutChart(rows, total, label, emptyText) {
    if (!total) return emptyState(null, emptyText, true);
    const svg = document.createElementNS(SVG_NS, "svg");
    svg.setAttribute("viewBox", "0 0 42 42"); svg.setAttribute("class", "donut"); svg.setAttribute("role", "img");
    svg.setAttribute("aria-label", label + " : " + total);
    const mkCircle = (cls) => {
      const c = document.createElementNS(SVG_NS, "circle");
      c.setAttribute("cx", "21"); c.setAttribute("cy", "21"); c.setAttribute("r", "15.9155"); c.setAttribute("class", cls);
      return c;
    };
    svg.append(mkCircle("track"));
    let acc = 0;
    for (const r of rows) {
      if (r.value <= 0) continue;
      const pct = (r.value / total) * 100;
      const c = mkCircle("seg " + r.key);
      c.setAttribute("stroke-dasharray", pct + " " + (100 - pct));
      c.setAttribute("stroke-dashoffset", String(25 - acc));
      svg.append(c);
      acc += pct;
    }
    const text = document.createElementNS(SVG_NS, "text");
    text.setAttribute("x", "21"); text.setAttribute("y", "21"); text.setAttribute("class", "donut-total");
    text.textContent = String(total);
    svg.append(text);
    const legend = h("ul", { class: "legend" }, rows.map((r) => h("li", {},
      h("span", { class: "dot " + r.key, "aria-hidden": "true" }), h("span", {}, r.label),
      h("span", { class: "count" }, r.value), h("span", { class: "pct" }, Math.round((r.value / total) * 100) + "%"))));
    return h("div", { class: "donut-wrap" }, svg, legend);
  }
  function kpiCard(label, value, ic, tone, sub) {
    return h("li", { class: "kpi tone-" + tone },
      h("span", { class: "kpi-icon" }, icon(ic)),
      h("div", { class: "kpi-body" }, h("span", { class: "kpi-label" }, label), h("strong", { class: "kpi-value" }, value),
        sub ? h("span", { class: "kpi-sub" }, sub) : null));
  }

  async function pageDashboard() {
    const t = T(), d = t.dash;
    if (!isStaff()) { location.hash = "#/tickets"; return; }
    loading();
    // Seules les statistiques sont indispensables ; le reste est facultatif et ne doit pas casser le tableau de bord.
    const optional = (p) => p.catch(() => null);
    let s, tickets, equipment, insights;
    try {
      [s, tickets, equipment, insights] = await Promise.all([
        api("/stats"), optional(api("/tickets")), optional(api("/equipment")), optional(api("/ai/insights"))]);
    } catch (err) { return failPage(err); }

    const countBy = (arr, key, val) => ((arr || []).find((x) => x[key] === val) || {}).count || 0;
    const equipInRepair = equipment ? equipment.filter((x) => x.status === "in_repair").length : 0;
    const kpis = h("ul", { class: "kpis" },
      kpiCard(d.total, s.total_tickets, "ticket", "navy"),
      kpiCard(d.newTickets, countBy(s.by_status, "status", "new"), "inbox", "info"),
      kpiCard(d.inProgress, countBy(s.by_status, "status", "in_progress"), "progress", "warn"),
      kpiCard(d.resolved, s.resolved_count, "check", "ok"),
      kpiCard(d.equipment, equipment ? equipment.length : t.ticket.none, "monitor", "gold",
        equipment && equipInRepair ? equipInRepair + " " + d.inRepair : null),
      kpiCard(d.overdue, s.overdue_maintenance, "wrench", s.overdue_maintenance > 0 ? "danger" : "ok"),
      kpiCard(d.avgRes, s.resolved_count ? fmtHours(s.avg_resolution_hours) : t.ticket.none, "clock", "navy"),
      kpiCard(d.avgResp, s.avg_first_response_hours > 0 ? fmtHours(s.avg_first_response_hours) : t.ticket.none, "timer", "navy"));

    const statusRows = STATUSES.map((k) => ({ key: k, label: t.status[k], value: countBy(s.by_status, "status", k) }));
    const statusTotal = statusRows.reduce((a, r) => a + r.value, 0);
    const byStatus = donutChart(statusRows, statusTotal, d.byStatus, d.noData);
    // La répartition par priorité est calculée à partir de la liste réelle des demandes.
    const byPriority = tickets
      ? barChart(PRIORITIES.map((k) => ({ cls: k, label: t.priority[k], value: tickets.filter((x) => x.priority === k).length })), d.noData)
      : emptyState(null, d.unavailable, true);
    const byCat = barChart([...(s.by_category || [])].sort((a, b) => b.count - a.count).map((x) => ({ label: t.category[x.category] || x.category, value: x.count })), d.noData);
    const topEq = barChart((s.top_equipment || []).map((x) => ({ cls: "accent", label: x.asset_tag + " · " + x.name, value: x.count })), d.noData);

    // Dernières demandes (les plus récentes d'abord)
    let recentBody;
    if (!tickets) recentBody = emptyState(null, d.unavailable, true);
    else if (!tickets.length) recentBody = emptyState(t.tickets.emptyTitle, t.tickets.emptyBody);
    else {
      const c = t.tickets.cols;
      const latest = [...tickets].sort((a, b) => new Date(b.created_at) - new Date(a.created_at)).slice(0, 6);
      recentBody = tableOf([c.id, c.title, c.category, c.priority, c.status, c.assignee, c.created, ""],
        latest.map((x) => h("tr", {},
          td(c.id, "num", "#" + x.id),
          td(c.title, null, h("a", { class: "row-link", href: "#/tickets/" + x.id }, x.title)),
          td(c.category, null, t.category[x.category]),
          td(c.priority, null, tag(x.priority, t.priority[x.priority])),
          td(c.status, null, tag(x.status, t.status[x.status])),
          td(c.assignee, null, x.assignee_name || t.ticket.none),
          td(c.created, "num", fmtDate(x.created_at)),
          td("", "actions", h("a", { class: "btn secondary sm", href: "#/tickets/" + x.id, "aria-label": t.view + " #" + x.id }, icon("eye"), t.view)))));
    }
    const recent = panel(d.recent, recentBody, {
      action: h("a", { class: "panel-link", href: "#/tickets" }, d.viewAll, icon("arrow", "flip")),
    });

    // Aperçu IA : même source (/ai/insights) et même contenu qu'auparavant ; échec silencieux et non bloquant.
    const ins = t.insights;
    const insightsHead = h("div", { class: "insights-head" }, h("span", { class: "icon-tile" }, icon("sparkles")), h("h2", {}, ins.title));
    const insightPanel = insights ? h("section", { class: "panel insights" }, insightsHead,
      h("div", { class: "pill-row" },
        h("span", { class: "pill" }, ins.analyzed + " :", h("strong", {}, insights.ticket_count)),
        h("span", { class: "pill" }, ins.open + " :", h("strong", {}, insights.open_count))),
      h("h3", {}, ins.recurring),
      (insights.recurring_issues || []).length
        ? h("ul", { class: "recurring" }, insights.recurring_issues.map((x) => h("li", {}, h("span", {}, x.title), h("span", {}, x.count + " " + ins.occurrences))))
        : h("p", { class: "note" }, ins.noneRecurring),
      h("h3", {}, ins.diagnostic),
      h("p", { class: "note" }, ins.disclaimer),
      h("div", { class: "diag-grid" }, (insights.diagnostic_steps || []).map((group) => h("div", { class: "diag-card" },
        h("h4", {}, t.category[group.category] || group.category),
        h("ul", {}, group.steps.map((step) => h("li", {}, step)))))))
      : h("section", { class: "panel insights" }, insightsHead, h("p", { class: "note" }, ins.unavailable));

    const today = new Intl.DateTimeFormat(t.locale, { dateStyle: "full" }).format(new Date());
    const refreshBtn = h("button", { type: "button", class: "btn secondary" }, icon("refresh"), t.refresh);
    refreshBtn.addEventListener("click", () => { setBusy(refreshBtn, true); pageDashboard(); });
    const head = h("div", { class: "page-head" },
      h("div", {}, h("h1", {}, d.title), h("p", {}, d.subtitle), h("span", { class: "date-line" }, icon("calendar"), today)),
      h("div", { class: "page-actions" }, refreshBtn,
        h("a", { class: "btn", href: "#/new" }, icon("plus"), t.nav.new)));

    mount(head, kpis,
      h("div", { class: "grid-charts" },
        panel(d.byStatus, byStatus), panel(d.byPriority, byPriority),
        panel(d.byCategory, byCat), panel(d.topEquipment, topEq)),
      recent, insightPanel);
  }

  // --- Utilisateurs (administrateur) ---
  async function pageUsers() {
    const t = T(), u = t.users;
    if (!isAdmin()) { location.hash = "#/tickets"; return; }
    loading();
    let list;
    try { state.staffUsers = null; list = await api("/users"); state.staffUsers = list; } catch (err) { return failPage(err); }
    const form = h("form", { class: "stack", novalidate: true });
    const name = h("input", { name: "full_name", maxlength: 100, required: true });
    const email = h("input", { name: "email", type: "email", required: true, dir: "ltr", autocomplete: "off" });
    const role = select("role", enumOptions(t.role, ["employee", "technician", "admin"]), "employee");
    const pass = h("input", { name: "password", type: "password", minlength: 8, required: true, dir: "ltr", autocomplete: "new-password" });
    const save = h("button", { class: "btn", type: "submit" }, u.save);
    form.append(h("div", { class: "row" }, field(u.name, name), field(u.email, email)),
      h("div", { class: "row" }, field(u.role, role), field(u.password, pass, u.passwordHint)), h("div", { class: "btn-row" }, save));
    form.addEventListener("submit", (ev) => {
      ev.preventDefault();
      submitForm(form, save, async () => {
        await api("/users", { method: "POST", body: { full_name: name.value, email: email.value, role: role.value, password: pass.value } });
        toast(u.saved); state.staffUsers = null; pageUsers();
      });
    });
    const c = u.cols;
    const table = tableOf([c.name, c.email, c.role, c.status], list.map((x) => h("tr", {},
      td(c.name, null, x.full_name), td(c.email, null, h("bdi", { dir: "ltr" }, x.email)),
      td(c.role, null, t.role[x.role]), td(c.status, null, tag(x.active ? "active" : "retired", x.active ? u.active : u.inactive)))));
    mount(pageHead(u.title, u.subtitle), h("section", { class: "panel form-panel" }, h("h2", {}, u.add), form), table);
  }

  // ---------- Routage ----------
  async function route() {
    const hash = location.hash || "#/tickets";
    const path = hash.slice(2).split("?")[0];
    const parts = path.split("/");
    if (state.token && !state.user) {
      try { state.user = await api("/me"); } catch (_) { state.token = null; store.del("sessionStorage", "token"); }
    }
    if (!state.user) {
      if (parts[0] !== "login") { location.hash = "#/login"; return; }
      return pageLogin();
    }
    renderBand();
    setMenu(false);
    window.scrollTo(0, 0);
    switch (true) {
      case parts[0] === "login": location.hash = "#/tickets"; return;
      case parts[0] === "tickets" && parts[1] != null: return pageTicket(Number(parts[1]));
      case parts[0] === "tickets": return pageTickets();
      case parts[0] === "new": return pageNewTicket();
      case parts[0] === "equipment" && parts[1] === "new": return pageEquipmentForm(null);
      case parts[0] === "equipment" && parts[1] != null: return pageEquipmentForm(Number(parts[1]));
      case parts[0] === "equipment": return pageEquipment();
      case parts[0] === "dashboard": return pageDashboard();
      case parts[0] === "users": return pageUsers();
      default: mount(h("p", {}, T().notFoundPage), h("a", { href: "#/tickets" }, T().nav.tickets));
    }
  }

  function toggleLang() {
    state.lang = state.lang === "ar" ? "fr" : "ar";
    store.set("localStorage", "lang", state.lang);
    route();
  }

  hydrateIcons();
  $("lang-toggle").addEventListener("click", toggleLang);
  $("logout").addEventListener("click", () => logout(false));
  window.addEventListener("hashchange", route);
  applyLang();
  route();
})();
