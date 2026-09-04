"use client";

import { useMemo, useState, type CSSProperties } from "react";
import {
  Activity, ArrowLeft, ArrowRight, Bike, Bot, CalendarDays, Check,
  ChevronRight, CircleUserRound, Clock3, Dumbbell, Flame, Footprints,
  HeartPulse, LibraryBig, Menu, Moon, MoreHorizontal,
  Play, Plus, Search, Settings, ShieldCheck, Sparkles, Trophy, Waves,
  Watch, Zap,
} from "lucide-react";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import {
  Sidebar, SidebarContent, SidebarFooter, SidebarGroup, SidebarGroupContent,
  SidebarGroupLabel, SidebarHeader, SidebarInset, SidebarMenu,
  SidebarMenuButton, SidebarMenuItem, SidebarProvider, SidebarTrigger,
} from "@/components/ui/sidebar";

type ScreenId =
  | "welcome" | "goals" | "sports" | "limits" | "firstWeek"
  | "today" | "readiness" | "recovery" | "nutrition"
  | "week" | "day" | "builder"
  | "library" | "exercise" | "technique"
  | "strength" | "swim" | "bike" | "basketball" | "summary"
  | "progress" | "sportProgress" | "coach"
  | "profile" | "devices" | "subscription";

type ScreenItem = { id: ScreenId; label: string };

const groups: { label: string; icon: typeof Activity; items: ScreenItem[] }[] = [
  { label: "Первый запуск", icon: Sparkles, items: [
    { id: "welcome", label: "Приветствие" }, { id: "goals", label: "Цель" },
    { id: "sports", label: "Виды спорта" }, { id: "limits", label: "Ограничения" },
    { id: "firstWeek", label: "Первый план" },
  ]},
  { label: "Сегодня", icon: Zap, items: [
    { id: "today", label: "Главная" }, { id: "readiness", label: "Готовность" },
    { id: "recovery", label: "Восстановление" }, { id: "nutrition", label: "Питание" },
  ]},
  { label: "План", icon: CalendarDays, items: [
    { id: "week", label: "Неделя" }, { id: "day", label: "День тренировки" },
    { id: "builder", label: "Конструктор" },
  ]},
  { label: "Энциклопедия", icon: LibraryBig, items: [
    { id: "library", label: "Каталог" }, { id: "exercise", label: "Упражнение" },
    { id: "technique", label: "Техника" },
  ]},
  { label: "Во время тренировки", icon: Dumbbell, items: [
    { id: "strength", label: "Зал" }, { id: "swim", label: "Плавание" },
    { id: "bike", label: "Велосипед" }, { id: "basketball", label: "Баскетбол" },
    { id: "summary", label: "Итоги" },
  ]},
  { label: "Результаты и система", icon: Trophy, items: [
    { id: "progress", label: "Прогресс" }, { id: "sportProgress", label: "По видам спорта" },
    { id: "coach", label: "AI-коуч" }, { id: "profile", label: "Профиль" },
    { id: "devices", label: "Устройства" }, { id: "subscription", label: "Подписка" },
  ]},
];

const flatScreens = groups.flatMap((group) => group.items);

function Header({ eyebrow, title, onBack, onMore }: { eyebrow: string; title: string; onBack?: () => void; onMore?: () => void }) {
  return <header className="app-header">
    <div className="header-title">
      {onBack && <button type="button" className="icon-button" aria-label="Вернуться назад" onClick={onBack}><ArrowLeft aria-hidden="true" /></button>}
      <div><p className="eyebrow">{eyebrow}</p><h1>{title}</h1></div>
    </div>
    <button type="button" className="icon-button" aria-label="Дополнительные действия" onClick={onMore}><MoreHorizontal aria-hidden="true" /></button>
  </header>;
}

function BottomNav({ active = "today", navigate }: { active?: "today" | "plan" | "base" | "progress" | "profile"; navigate: (id: ScreenId) => void }) {
  const items = [
    { id: "today", screen: "today", label: "Сегодня", icon: Zap }, { id: "plan", screen: "week", label: "План", icon: CalendarDays },
    { id: "base", screen: "library", label: "База", icon: LibraryBig }, { id: "progress", screen: "progress", label: "Прогресс", icon: Trophy },
    { id: "profile", screen: "profile", label: "Профиль", icon: CircleUserRound },
  ] as const;
  return <nav className="bottom-nav" aria-label="Основная навигация">{items.map((item) => {
    const Icon = item.icon;
    return <button type="button" key={item.id} className={active === item.id ? "active" : ""} aria-current={active === item.id ? "page" : undefined} onClick={() => navigate(item.screen)}><Icon aria-hidden="true" /><span>{item.label}</span></button>;
  })}</nav>;
}

function Metric({ label, value, detail }: { label: string; value: string; detail?: string }) {
  return <div className="metric"><span>{label}</span><strong>{value}</strong>{detail && <small>{detail}</small>}</div>;
}

function SportIcon({ type }: { type: "strength" | "swim" | "bike" | "basketball" }) {
  const map = { strength: Dumbbell, swim: Waves, bike: Bike, basketball: Activity };
  const Icon = map[type];
  return <span className={"sport-icon " + type}><Icon /></span>;
}

function WeekStrip({ selected = 3 }: { selected?: number }) {
  const days = [["ПН","31"],["ВТ","1"],["СР","2"],["ЧТ","3"],["ПТ","4"],["СБ","5"],["ВС","6"]];
  return <div className="week-strip">{days.map((day, index) => <div key={day[0]} className={selected === index ? "selected" : ""}><span>{day[0]}</span><b>{day[1]}</b>{[0,2,3,5].includes(index) && <i />}</div>)}</div>;
}

function ExerciseArt() {
  return <div className="exercise-art">
    <img src="/exercise-lat-pulldown.png" alt="Три фазы тяги верхнего блока нейтральным хватом" />
    <span className="verified"><ShieldCheck /> Проверено экспертом</span>
  </div>;
}

function ScreenCanvas({ screen, navigate, goBack, announce }: { screen: ScreenId; navigate: (id: ScreenId) => void; goBack: () => void; announce: (message: string) => void }) {
  const [goal, setGoal] = useState("Стать сильнее");
  const [sports, setSports] = useState(["strength", "swim", "bike", "basketball"]);
  const [limits, setLimits] = useState(["Левое плечо", "Правое колено"]);
  const [phase, setPhase] = useState(0);
  const [setNumber, setSetNumber] = useState(2);
  const [restSkipped, setRestSkipped] = useState(false);
  const [swimLap, setSwimLap] = useState(3);
  const [paused, setPaused] = useState(false);
  const [shots, setShots] = useState({ hits: 7, attempts: 10 });
  const [effort, setEffort] = useState(4);
  const [period, setPeriod] = useState<"month" | "year">("year");
  const [libraryQuery, setLibraryQuery] = useState("");
  const [librarySport, setLibrarySport] = useState("Все");
  const [coachInput, setCoachInput] = useState("");
  const more = () => announce("Дополнительные действия доступны в следующем меню");
  const toggleSport = (id: string) => setSports((current) => current.includes(id) ? current.filter((item) => item !== id) : [...current, id]);
  const toggleLimit = (label: string) => setLimits((current) => current.includes(label) ? current.filter((item) => item !== label) : [...current, label]);
  const phases = [
    { kicker: "01 · СТАРТОВАЯ ПОЗИЦИЯ", title: "Грудь вверх, плечи вниз", copy: "Возьми рукоять нейтральным хватом. Слегка отклони корпус, напряги пресс и опусти плечи от ушей." },
    { kicker: "02 · ТЯГА", title: "Локти ведут движение", copy: "Потяни рукоять к верхней части груди. Своди лопатки и не уводи локти далеко назад." },
    { kicker: "03 · ВОЗВРАТ", title: "Верни вес под контролем", copy: "Плавно выпрями руки, сохраняя корпус неподвижным и не поднимая плечи к ушам." },
  ];
  if (screen === "welcome") return <div className="phone-screen onboarding-screen">
    <div className="brand-mark"><Activity /></div>
    <div className="welcome-copy"><p className="eyebrow">ATHLETICA AI</p><h1>Один план.<br/>Весь твой спорт.</h1><p>Зал, плавание, велосипед и баскетбол работают вместе — без перегруза и хаоса.</p></div>
    <div className="sport-orbits"><SportIcon type="strength"/><SportIcon type="swim"/><SportIcon type="bike"/><SportIcon type="basketball"/></div>
    <Button className="primary-action" onClick={() => navigate("goals")}>Собрать мой план <ArrowRight /></Button>
    <button className="text-action" onClick={() => announce("Вход будет подключён после готовности авторизации")}>У меня уже есть аккаунт</button>
  </div>;

  if (screen === "goals") return <div className="phone-screen onboarding-screen">
    <div className="step-header"><button type="button" className="icon-button" aria-label="Вернуться назад" onClick={goBack}><ArrowLeft aria-hidden="true" /></button><span>1 из 3</span></div>
    <Progress value={33} className="progress-line" />
    <p className="eyebrow">ГЛАВНАЯ ЦЕЛЬ</p><h1 className="onboarding-title">К чему идём в первую очередь?</h1>
    <div className="choice-stack">
      {["Стать сильнее","Набрать мышечную массу","Улучшить выносливость","Подготовиться к событию","Вернуться в форму"].map((x)=><button type="button" aria-pressed={goal===x} className={"choice " + (goal===x?"selected":"")} key={x} onClick={() => setGoal(x)}><span>{x}</span>{goal===x?<Check aria-hidden="true" />:<ChevronRight aria-hidden="true" />}</button>)}
    </div>
    <Button className="primary-action" onClick={() => navigate("sports")}>Продолжить</Button>
  </div>;

  if (screen === "sports") return <div className="phone-screen onboarding-screen">
    <div className="step-header"><button type="button" className="icon-button" aria-label="Вернуться назад" onClick={goBack}><ArrowLeft aria-hidden="true" /></button><span>2 из 3</span></div>
    <Progress value={66} className="progress-line" />
    <p className="eyebrow">ТВОИ НАПРАВЛЕНИЯ</p><h1 className="onboarding-title">Что должно жить в одном плане?</h1>
    <p className="lead">Выбери всё, чем занимаешься. Детали можно изменить в профиле.</p>
    <div className="sport-grid">
      {[
        ["strength","Силовые","3× в неделю"],["swim","Плавание","2× в неделю"],
        ["bike","Велосипед","1–2× в неделю"],["basketball","Баскетбол","По выходным"]
      ].map((x)=><button type="button" aria-pressed={sports.includes(x[0])} className={"sport-choice "+(sports.includes(x[0])?"selected":"")} key={x[0]} onClick={() => toggleSport(x[0])}><SportIcon type={x[0] as "strength"|"swim"|"bike"|"basketball"}/><div><b>{x[1]}</b><span>{x[2]}</span></div>{sports.includes(x[0])&&<Check aria-hidden="true" />}</button>)}
    </div>
    <Button className="primary-action" onClick={() => navigate("limits")}>Продолжить</Button>
  </div>;

  if (screen === "limits") return <div className="phone-screen onboarding-screen">
    <div className="step-header"><button type="button" className="icon-button" aria-label="Вернуться назад" onClick={goBack}><ArrowLeft aria-hidden="true" /></button><span>3 из 3</span></div>
    <Progress value={100} className="progress-line" />
    <p className="eyebrow">БЕЗОПАСНОСТЬ</p><h1 className="onboarding-title">Что важно учитывать?</h1>
    <div className="body-limit">
      <div className="body-figure"><span className="head"/><span className="torso"/><span className="arms"/><span className="legs"/><i className="hotspot shoulder"/><i className="hotspot knee"/></div>
      <div className="limit-list"><button type="button" aria-pressed={limits.includes("Левое плечо")} className={"limit-tag "+(limits.includes("Левое плечо")?"active":"")} onClick={() => toggleLimit("Левое плечо")}>Левое плечо <b>3/5</b></button><button type="button" aria-pressed={limits.includes("Правое колено")} className={"limit-tag "+(limits.includes("Правое колено")?"active":"")} onClick={() => toggleLimit("Правое колено")}>Правое колено <b>2/5</b></button><button type="button" className="limit-tag" onClick={() => announce("Выберите область на карте тела") }><Plus aria-hidden="true"/> Добавить область</button></div>
    </div>
    <div className="notice"><ShieldCheck/><p><b>Мы адаптируем упражнения</b><span>Приложение не заменяет врача и не ставит диагноз.</span></p></div>
    <Button className="primary-action" onClick={() => navigate("firstWeek")}>Учесть в плане</Button>
  </div>;

  if (screen === "firstWeek") return <div className="phone-screen">
    <Header eyebrow="ПЛАН ГОТОВ" title="Твоя первая неделя" onMore={more} />
    <div className="plan-hero"><Sparkles/><div><strong>Баланс нагрузки</strong><span>4 тренировки · 5 ч 10 мин</span></div><b>92%</b></div>
    <div className="timeline">
      {[["ПН","strength","Верх тела","65 мин"],["СР","swim","Техника кроля","50 мин"],["ЧТ","strength","Ноги + кор","70 мин"],["СБ","bike","Аэробная база","75 мин"]].map((x,i)=><button type="button" className="timeline-row" key={x[0]} onClick={() => navigate(x[1]==="strength"?"day":x[1] as ScreenId)}><span className="day-number">{x[0]}</span><SportIcon type={x[1] as "strength"|"swim"|"bike"|"basketball"}/><div><b>{x[2]}</b><span>{x[3]}</span></div>{i===2&&<em>адаптировано</em>}<ChevronRight/></button>)}
    </div>
    <div className="ai-note"><Bot/><div><b>Почему так?</b><p>Развёл тяжёлую нагрузку на ноги и велосипед, а плавание поставил между силовыми днями.</p></div></div>
    <Button className="primary-action" onClick={() => navigate("today")}>Начать неделю</Button>
  </div>;

  if (screen === "today") return <div className="phone-screen has-nav">
    <Header eyebrow="ЧЕТВЕРГ · 3 СЕНТЯБРЯ" title="Добрый день, Никита" onMore={more} />
    <WeekStrip />
    <button className="readiness-card" onClick={() => navigate("readiness")}>
      <div><span>ГОТОВНОСТЬ</span><strong>78</strong><small>хорошая</small></div>
      <div className="readiness-track"><i style={{width:"78%"}}/><p>Сон снизил интенсивность на 5%</p></div><ChevronRight />
    </button>
    <section className="session-card">
      <div className="session-top"><SportIcon type="strength"/><span>СЕГОДНЯ · ЗАЛ</span><b>65 мин</b></div>
      <h2>Верх тела · сила</h2>
      <p>6 упражнений · грудь, спина, плечи</p>
      <div className="session-actions"><Button onClick={() => navigate("strength")}><Play/> Начать</Button><button onClick={() => announce("Тренировка сокращена до 40 минут")}><Clock3/> Укоротить</button><button onClick={() => announce("Укажите болезненную область — план будет адаптирован")}><HeartPulse/> Есть боль</button></div>
    </section>
    <div className="daily-grid"><button className="mini-panel recovery-panel" onClick={() => navigate("recovery")}><span>Восстановление</span><strong>Ноги 62%</strong><small>после велосипеда</small></button><button className="mini-panel nutrition-panel" onClick={() => navigate("nutrition")}><span>Питание</span><strong>142 / 190 г</strong><small>белка сегодня</small></button></div>
    <button type="button" className="coach-strip" onClick={() => navigate("coach")}><Bot/><div><b>AI предлагает</b><span>Оставить 2 повтора в запасе</span></div><ChevronRight/></button>
    <BottomNav active="today" navigate={navigate} />
  </div>;

  if (screen === "readiness") return <div className="phone-screen">
    <Header eyebrow="ВОССТАНОВЛЕНИЕ" title="Готовность сегодня" onBack={goBack} onMore={more} />
    <div className="score-hero"><div><span>READINESS</span><strong>78</strong><small>из 100 · уверенность 86%</small></div><div className="status-pill">Хорошая</div></div>
    <section className="open-section"><div className="section-heading"><h2>Почему 78</h2><span>−6 ко вчера</span></div>
      {[["Сон","6 ч 49 мин","−4.8","down"],["Энергия","4 из 5","+2.0","up"],["Стресс","3 из 5","−1.5","down"],["Нагрузка ног","12 подходов / 48 ч","−1.2","down"]].map(x=><div className="factor" key={x[0]}><span className={"factor-dot "+x[3]}/><div><b>{x[0]}</b><small>{x[1]}</small></div><strong>{x[2]}</strong></div>)}
    </section>
    <div className="adjust-card"><Sparkles/><div><span>АДАПТАЦИЯ ПЛАНА</span><b>Интенсивность −5%</b><p>Рабочие веса снижены, общий объём сохранён.</p></div></div>
    <h2 className="section-title">Контекст</h2>
    <div className="metric-grid"><Metric label="Сон" value="6:49" detail="норма 7:31"/><Metric label="Пульс покоя" value="54" detail="−2 к норме"/><Metric label="Шаги" value="10 420"/><Metric label="HRV" value="48 мс" detail="в норме"/></div>
    <Button className="secondary-action" onClick={() => navigate("today")}>Вернуться к плану</Button>
  </div>;

  if (screen === "recovery") return <div className="phone-screen">
    <Header eyebrow="КАРТА ТЕЛА" title="Восстановление" onBack={goBack} onMore={more} />
    <div className="recovery-map">
      <div className="body-figure large"><span className="head"/><span className="torso"/><span className="arms"/><span className="legs"/><i className="muscle-zone chest"/><i className="muscle-zone quad-left"/><i className="muscle-zone quad-right"/></div>
      <div className="map-legend"><span><i className="fresh"/>Готово</span><span><i className="medium"/>Средне</span><span><i className="tired"/>Усталость</span></div>
    </div>
    <div className="recovery-list"><div><span className="zone-mark orange"/><p><b>Ноги</b><small>12 подходов · soreness 3/5</small></p><strong>62%</strong></div><div><span className="zone-mark blue"/><p><b>Спина</b><small>4 подхода · готова к нагрузке</small></p><strong>88%</strong></div><div><span className="zone-mark green"/><p><b>Грудь</b><small>6 подходов · восстановлена</small></p><strong>92%</strong></div></div>
    <div className="ai-note compact"><Bot/><div><b>Лучший выбор сегодня</b><p>Верх тела без тяжёлой осевой нагрузки.</p></div></div>
  </div>;

  if (screen === "nutrition") return <div className="phone-screen has-nav">
    <Header eyebrow="СЕГОДНЯ" title="Питание" onMore={more} />
    <div className="nutrition-hero"><div><span>1 840</span><small>из 2 650 ккал</small></div><div className="macro-bars"><p><b>Б</b><i><em style={{width:"75%"}}/></i><span>142 / 190</span></p><p><b>Ж</b><i><em style={{width:"77%"}}/></i><span>58 / 75</span></p><p><b>У</b><i><em style={{width:"67%"}}/></i><span>189 / 280</span></p></div></div>
    <Button className="ai-food" onClick={() => announce("Открылся AI-ввод продукта")}><Sparkles/> Добавить через AI</Button>
    <div className="meal-list"><div className="meal-title"><h2>Завтрак</h2><span>349 ккал</span></div><button><span><b>Творог 5%</b><small>200 г</small></span><strong>242</strong></button><button><span><b>Банан</b><small>120 г</small></span><strong>107</strong></button><div className="meal-title"><h2>Обед</h2><span>615 ккал</span></div><button><span><b>Рис + курица</b><small>410 г</small></span><strong>615</strong></button><div className="meal-title"><h2>Ужин</h2><span>0 ккал</span></div><button className="empty-meal"><Plus/> Добавить продукт</button></div>
    <BottomNav active="today" navigate={navigate} />
  </div>;

  if (screen === "week") return <div className="phone-screen has-nav">
    <Header eyebrow="НЕДЕЛЯ 1 · БАЗОВЫЙ ЦИКЛ" title="План" onMore={more} />
    <WeekStrip selected={3}/>
    <div className="week-load"><div><span>НАГРУЗКА НЕДЕЛИ</span><strong>312 / 420</strong><small>оптимальная зона</small></div><div className="load-bars">{[35,78,22,64,18,82,20].map((v,i)=><i key={i}><em style={{height:v+"%"}}/></i>)}</div></div>
    <div className="plan-list">
      {[["Сегодня","strength","Верх тела · сила","18:30 · 65 мин"],["Пятница","rest","Восстановление","Мобилити · 20 мин"],["Суббота","bike","Аэробная база","75 мин · зона 2"],["Воскресенье","basketball","Бросок + игра","80 мин"]].map(x=><button key={x[0]} onClick={()=>navigate(x[1]==="strength"?"day":x[1]==="rest"?"recovery":x[1] as ScreenId)}><span className="date-col">{x[0]}</span>{x[1]==="rest"?<span className="sport-icon rest"><Moon/></span>:<SportIcon type={x[1] as "strength"|"swim"|"bike"|"basketball"}/>}<div><b>{x[2]}</b><span>{x[3]}</span></div><ChevronRight/></button>)}
    </div>
    <Button className="secondary-action" onClick={() => announce("Выберите вид активности и свободное время")}><Plus/> Добавить активность</Button>
    <BottomNav active="plan" navigate={navigate}/>
  </div>;

  if (screen === "day") return <div className="phone-screen">
    <Header eyebrow="ЧЕТВЕРГ · ЗАЛ" title="Верх тела · сила" onBack={goBack} onMore={more} />
    <div className="workout-meta"><span><Clock3/>65 мин</span><span><Flame/>Средняя</span><span><Dumbbell/>6 упражнений</span></div>
    <div className="adaptation"><Bot/><div><b>Адаптировано под готовность 78</b><span>Рабочие веса −5%, объём без изменений</span></div><button onClick={() => announce("Причина: сон короче нормы и нагрузка на ноги за последние 48 часов")}>Почему?</button></div>
    <div className="exercise-list">
      {[["01","Жим гантелей лёжа","3 × 8–10","Грудь"],["02","Тяга верхнего блока","3 × 10–12","Спина"],["03","Жим в тренажёре","3 × 10","Грудь"],["04","Тяга горизонтального блока","3 × 12","Спина"],["05","Разведения в стороны","3 × 14","Плечи"],["06","Pallof press","3 × 12","Кор"]].map((x,i)=><button key={x[0]} onClick={()=>i===1?navigate("exercise"):announce(`Открыто упражнение: ${x[1]}`)}><span className="exercise-index">{x[0]}</span><div><b>{x[1]}</b><small>{x[2]} · {x[3]}</small></div>{i===1?<span className="safe-tag">безопасно</span>:<ChevronRight/>}</button>)}
    </div>
    <Button className="primary-action" onClick={()=>navigate("strength")}><Play/> Начать тренировку</Button>
    <button className="text-action" onClick={()=>navigate("builder")}>Редактировать план</button>
  </div>;

  if (screen === "builder") return <div className="phone-screen">
    <Header eyebrow="КОНСТРУКТОР" title="Редактировать тренировку" onBack={goBack} onMore={more} />
    <div className="builder-summary"><span>6 упражнений</span><span>18 подходов</span><span>≈ 65 мин</span></div>
    <div className="builder-list">{["Жим гантелей лёжа","Тяга верхнего блока","Жим в тренажёре","Тяга горизонтального блока","Разведения в стороны"].map((x,i)=><div key={x}><Menu/><span className="exercise-index">0{i+1}</span><div><b>{x}</b><small>3 подхода · 8–12</small></div><button><MoreHorizontal/></button></div>)}</div>
    <Button className="secondary-action" onClick={()=>navigate("library")}><Plus/> Добавить упражнение</Button>
    <div className="builder-footer"><button onClick={() => announce("Шаблон сохранён")}>Сохранить как шаблон</button><Button onClick={() => { announce("Тренировка сохранена"); navigate("day"); }}>Сохранить тренировку</Button></div>
  </div>;

  if (screen === "library") return <div className="phone-screen has-nav">
    <Header eyebrow="1 248 МАТЕРИАЛОВ" title="Энциклопедия" onMore={more} />
    <label className="search-field"><Search aria-hidden="true"/><span className="sr-only">Поиск упражнений</span><input type="search" value={libraryQuery} onChange={(event) => setLibraryQuery(event.target.value)} placeholder="Упражнение, мышца или навык" /></label>
    <div className="sport-tabs" role="tablist" aria-label="Фильтр по спорту">{["Все","Зал","Плавание","Вело","Баскетбол"].map((x)=><button type="button" role="tab" aria-selected={librarySport===x} className={librarySport===x?"active":""} key={x} onClick={() => setLibrarySport(x)}>{x}</button>)}</div>
    <section className="body-explorer"><div><p className="eyebrow">ПОИСК ПО ТЕЛУ</p><h2>Какая зона?</h2><p>Нажми на область или выбери движение</p><Button variant="outline">Открыть карту тела</Button></div><div className="mini-body"><span className="head"/><span className="torso"/><span className="arms"/><span className="legs"/><i className="hotspot shoulder"/></div></section>
    <div className="section-heading"><h2>Для сегодняшней тренировки</h2><button>Все</button></div>
    <button className="featured-exercise" onClick={()=>navigate("exercise")}><div className="featured-thumb"><img src="/exercise-lat-pulldown.png" alt="Тяга верхнего блока"/></div><div><span>СПИНА · СРЕДНИЙ</span><b>Тяга верхнего блока</b><small>Техника · ошибки · замены</small></div><ChevronRight/></button>
    <h2 className="section-title">По цели</h2>
    <div className="goal-grid"><button><Flame/><b>Сила</b><span>186 упражнений</span></button><button><Waves/><b>Техника плавания</b><span>74 упражнения</span></button><button><Footprints/><b>Мобильность</b><span>93 упражнения</span></button><button><HeartPulse/><b>Без боли</b><span>Подбор замены</span></button></div>
    <BottomNav active="base" navigate={navigate}/>
  </div>;

  if (screen === "exercise") return <div className="phone-screen">
    <Header eyebrow="СПИНА · ТРЕНАЖЁР" title="Тяга верхнего блока" onBack={goBack} onMore={more} />
    <ExerciseArt/>
    <div className="exercise-badges"><span>Средний уровень</span><span>Нейтральный хват</span><span className="safe">Плечо: безопасно</span></div>
    <section className="open-section exercise-copy"><h2>Зачем выполнять</h2><p>Развивает широчайшие и учит сводить лопатки без лишней нагрузки на плечевой сустав.</p></section>
    <div className="muscle-row"><div><span>ОСНОВНЫЕ</span><b>Широчайшие</b></div><div><span>ПОМОГАЮТ</span><b>Бицепс · ромбовидные</b></div></div>
    <div className="link-stack"><button onClick={()=>navigate("technique")}><span><b>Пошаговая техника</b><small>3 фазы движения</small></span><ChevronRight/></button><button onClick={() => announce("Показаны 4 частые ошибки техники")}><span><b>Частые ошибки</b><small>4 ключевых момента</small></span><ChevronRight/></button><button onClick={() => announce("Показаны 6 вариантов прогрессии")}><span><b>Регрессии и прогрессии</b><small>6 вариантов</small></span><ChevronRight/></button></div>
    <div className="dual-actions"><Button variant="outline" onClick={() => announce("Упражнение добавлено в избранное")}>В избранное</Button><Button onClick={()=>navigate("strength")}>Добавить в план</Button></div>
  </div>;

  if (screen === "technique") return <div className="phone-screen">
    <Header eyebrow="ТЕХНИКА · 3 ФАЗЫ" title="Выполняй точно" onBack={goBack} onMore={more} />
    <ExerciseArt/>
    <div className="phase-tabs" role="tablist" aria-label="Фазы упражнения">{["Старт","Тяга","Возврат"].map((label,index)=><button type="button" role="tab" aria-selected={phase===index} className={phase===index?"active":""} key={label} onClick={() => setPhase(index)}><b>0{index+1}</b><span>{label}</span></button>)}</div>
    <section className="phase-copy" aria-live="polite"><span>{phases[phase].kicker}</span><h2>{phases[phase].title}</h2><p>{phases[phase].copy}</p></section>
    <div className="cue-list"><div><Check/><span><b>Локти</b> идут вниз вдоль корпуса</span></div><div><Check/><span><b>Дыхание</b> выдох во время тяги</span></div><div className="warning"><HeartPulse/><span><b>Стоп</b> если появляется острая боль в плече</span></div></div>
    <Button className="primary-action" onClick={()=>navigate("strength")}><Play/> Начать упражнение</Button>
  </div>;

  if (screen === "strength") return <div className="phone-screen active-workout">
    <div className="workout-top"><button type="button" className="icon-button" aria-label="Вернуться назад" onClick={goBack}><ArrowLeft aria-hidden="true"/></button><div><span>2 ИЗ 6 · ЗАЛ</span><b>Тяга верхнего блока</b></div><button type="button" className="icon-button" aria-label="Дополнительные действия" onClick={more}><MoreHorizontal aria-hidden="true"/></button></div>
    <div className="rest-timer"><span>ОТДЫХ ДО СЛЕДУЮЩЕГО</span><strong role="timer">{restSkipped?"ГОТОВО":"01:18"}</strong><button onClick={() => setRestSkipped(true)}>Пропустить</button></div>
    <div className="compact-art"><img src="/exercise-lat-pulldown.png" alt="Техника упражнения"/><button onClick={()=>navigate("technique")}><Play/> Техника</button></div>
    <div className="previous-result"><span>В прошлый раз</span><b>62,5 кг × 11</b><small>RIR 2</small></div>
    <div className="set-table"><div className="set-head"><span>ПОДХОД</span><span>ВЕС, КГ</span><span>ПОВТОРЫ</span><span>RIR</span></div><div className="set-done"><b>1</b><span>60</span><span>12</span><span>2</span><Check/></div><div className="set-current"><b>2</b><button>62,5</button><button>10</button><button>2</button></div><div><b>3</b><span>62,5</span><span>—</span><span>—</span></div></div>
    <Button className="primary-action complete-set" onClick={() => { if (setNumber >= 3) navigate("summary"); else { setSetNumber(setNumber+1); setRestSkipped(false); announce(`Подход ${setNumber} завершён`); } }}><Check/> {setNumber >= 3?"Завершить упражнение":"Завершить подход"}</Button>
    <div className="quick-workout-actions"><button onClick={() => announce("Остановитесь. Выберите замену без боли или завершите тренировку")}><HeartPulse/> Больно</button><button onClick={() => announce("Подбираем свободную замену тренажёра")}><Dumbbell/> Занято</button><button onClick={() => announce("Следующий подход снижен на 5%") }><Zap/> Тяжело</button></div>
    <div className="workout-footer"><span>18:42</span><Progress value={32}/><button onClick={()=>navigate("summary")}>Завершить</button></div>
  </div>;

  if (screen === "swim") return <div className="phone-screen active-workout swim-mode">
    <div className="workout-top"><button type="button" className="icon-button" aria-label="Вернуться назад" onClick={goBack}><ArrowLeft aria-hidden="true"/></button><div><span>БЛОК 2 ИЗ 5 · БАССЕЙН 25 М</span><b>Техника кроля</b></div><button type="button" className="icon-button" aria-label="Дополнительные действия" onClick={more}><MoreHorizontal aria-hidden="true"/></button></div>
    <div className="sport-live-hero"><Waves/><span>СЕЙЧАС</span><strong>6 × 50 м</strong><h2>Кроль · длинное скольжение</h2><p>Отдых 20 сек · лёгкий темп</p></div>
    <div className="lap-counter"><button aria-label="Предыдущий отрезок" onClick={() => setSwimLap(Math.max(1,swimLap-1))}>−</button><div><span>ОТРЕЗОК</span><strong aria-live="polite">{swimLap} / 6</strong></div><button aria-label="Следующий отрезок" onClick={() => setSwimLap(Math.min(6,swimLap+1))}>+</button></div>
    <div className="live-metrics"><Metric label="Дистанция" value="650 м"/><Metric label="Время" value="18:42"/><Metric label="Темп" value="2:04"/><Metric label="Пульс" value="132"/></div>
    <div className="tech-cue"><Sparkles/><p><b>Фокус на технике</b><span>Вытянись вперёд до начала захвата воды.</span></p></div>
    <Button className="primary-action" onClick={() => swimLap>=6?navigate("summary"):setSwimLap(swimLap+1)}><Check/> {swimLap>=6?"Завершить тренировку":"Отрезок выполнен"}</Button>
    <div className="quick-workout-actions"><button onClick={() => announce("Выйдите из воды и мягко расслабьте мышцу. Тренировка поставлена на паузу")}><HeartPulse/> Судорога</button><button onClick={() => setPaused(!paused)}><Clock3/> {paused?"Продолжить":"Пауза"}</button><button onClick={() => setSwimLap(Math.min(6,swimLap+1))}><ArrowRight/> Пропустить</button></div>
  </div>;

  if (screen === "bike") return <div className="phone-screen active-workout bike-mode">
    <div className="workout-top"><button type="button" className="icon-button" aria-label="Вернуться назад" onClick={goBack}><ArrowLeft aria-hidden="true"/></button><div><span>ВЕЛО · ЗОНА 2</span><b>Аэробная база</b></div><button type="button" className="icon-button" aria-label="Дополнительные действия" onClick={more}><MoreHorizontal aria-hidden="true"/></button></div>
    <div className="bike-dashboard"><span>СКОРОСТЬ</span><strong>{paused?"0,0":"28,4"}</strong><small>км/ч</small><div className="zone-line"><i style={{width:paused?"0%":"64%"}}/><em>{paused?"Пауза":"Зона 2"}</em></div></div>
    <div className="live-metrics"><Metric label="Время" value="42:18"/><Metric label="Дистанция" value="20.1 км"/><Metric label="Пульс" value="138"/><Metric label="Каденс" value="88"/></div>
    <section className="interval-card"><span>ТЕКУЩИЙ ИНТЕРВАЛ · 12:40</span><h2>Держи ровную мощность</h2><div><b>Цель</b><strong>150–175 Вт</strong></div><div><b>Сейчас</b><strong>168 Вт</strong></div></section>
    <div className="safety-note"><ShieldCheck/><p><b>Экран безопасности</b><span>Крупные показатели, управление голосом и одной рукой.</span></p></div>
    <div className="dual-actions"><Button variant="outline" onClick={() => setPaused(!paused)}><Clock3/> {paused?"Продолжить":"Пауза"}</Button><Button onClick={() => navigate("summary")}><Check/> Завершить</Button></div>
  </div>;

  if (screen === "basketball") return <div className="phone-screen active-workout basket-mode">
    <div className="workout-top"><button type="button" className="icon-button" aria-label="Вернуться назад" onClick={goBack}><ArrowLeft aria-hidden="true"/></button><div><span>УПРАЖНЕНИЕ 3 ИЗ 7</span><b>Броски после дриблинга</b></div><button type="button" className="icon-button" aria-label="Дополнительные действия" onClick={more}><MoreHorizontal aria-hidden="true"/></button></div>
    <div className="court"><div className="court-line"/><div className="court-circle"/><span className="shot one"/><span className="shot two"/><span className="shot three"/><i className="route"/></div>
    <div className="drill-target"><span>СЕРИЯ</span><strong aria-live="polite">{shots.hits} / {shots.attempts}</strong><small>с правой стороны</small></div>
    <div className="shot-controls"><button className="miss" onClick={() => setShots({...shots,attempts:shots.attempts+1})}>Промах</button><button className="hit" onClick={() => setShots({hits:shots.hits+1,attempts:shots.attempts+1})}><Check/> Попал</button></div>
    <div className="live-metrics"><Metric label="Точность" value={`${Math.round(shots.hits/shots.attempts*100)}%`}/><Metric label="Попадания" value={`${shots.hits} / ${shots.attempts}`}/><Metric label="Серия" value="5"/><Metric label="Время" value="18:04"/></div>
    <div className="tech-cue"><Sparkles/><p><b>Подсказка</b><span>Последний шаг короче — так легче остановить корпус.</span></p></div><Button className="secondary-action" onClick={() => navigate("summary")}><Check/> Завершить тренировку</Button>
  </div>;

  if (screen === "summary") return <div className="phone-screen summary-screen">
    <div className="success-mark"><Check/></div><p className="eyebrow">ТРЕНИРОВКА ЗАВЕРШЕНА</p><h1>Сильная работа,<br/>Никита.</h1>
    <div className="summary-hero"><div><span>ОБЪЁМ</span><strong>8 420</strong><small>кг</small></div><div><span>ВРЕМЯ</span><strong>61:24</strong><small>мин</small></div></div>
    <div className="pr-card"><Trophy/><div><span>ЛИЧНЫЙ РЕКОРД</span><b>Жим гантелей · 42,5 кг × 10</b></div></div>
    <div className="feeling"><h2>Как ощущалась нагрузка?</h2><div>{[1,2,3,4,5].map(i=><button type="button" aria-pressed={effort===i} className={i===effort?"selected":""} key={i} onClick={() => setEffort(i)}>{i}</button>)}</div><p>{effort<=2?"Легко":effort===3?"Комфортная нагрузка":effort===4?"Тяжело, но с запасом":"Очень тяжело"}</p></div>
    <div className="recovery-advice"><Moon/><div><b>Следующий шаг</b><span>30 г белка и спокойная прогулка. Завтра — восстановление.</span></div></div>
    <Button className="primary-action" onClick={()=>navigate("progress")}>Посмотреть прогресс</Button>
  </div>;

  if (screen === "progress") return <div className="phone-screen has-nav">
    <Header eyebrow="ПОСЛЕДНИЕ 4 НЕДЕЛИ" title="Прогресс" onMore={more} />
    <div className="progress-lead"><div><span>УСПЕШНЫЕ НЕДЕЛИ</span><strong>3 / 4</strong><small>цель — выполнить ≥70% плана</small></div><Trophy/></div>
    <section className="chart-card"><div className="section-heading"><h2>Общая нагрузка</h2><button onClick={() => announce("Период: последние 4 недели")}>4 недели</button></div><div className="line-chart" role="img" aria-label="Общая нагрузка выросла на 12 процентов за четыре недели"><i/><span className="chart-line"/><em className="point p1"/><em className="point p2"/><em className="point p3"/><em className="point p4"/></div><div className="chart-labels"><span>5 авг</span><span>12 авг</span><span>19 авг</span><span>26 авг</span></div><p><Sparkles/> Нагрузка выросла на 12% без падения готовности.</p></section>
    <div className="metric-grid progress-metrics"><Metric label="Тренировки" value="18" detail="+3 к прошлому периоду"/><Metric label="Время" value="21 ч" detail="+9%"/><Metric label="Серия" value="12 дней"/><Metric label="Рекорды" value="6" detail="за 4 недели"/></div>
    <h2 className="section-title">По направлениям</h2>
    <div className="sport-progress-list"><button onClick={()=>navigate("sportProgress")}><SportIcon type="strength"/><div><b>Силовые</b><span>Объём +14%</span></div><strong>8</strong><ChevronRight/></button><button onClick={() => announce("Открыта аналитика плавания")}><SportIcon type="swim"/><div><b>Плавание</b><span>Темп 100 м −4 сек</span></div><strong>6</strong><ChevronRight/></button><button onClick={() => announce("Открыта аналитика велосипеда")}><SportIcon type="bike"/><div><b>Велосипед</b><span>148 км за месяц</span></div><strong>4</strong><ChevronRight/></button></div>
    <BottomNav active="progress" navigate={navigate}/>
  </div>;

  if (screen === "sportProgress") return <div className="phone-screen">
    <Header eyebrow="СИЛОВЫЕ · 8 ТРЕНИРОВОК" title="Стало больше силы" onBack={goBack} onMore={more} />
    <div className="achievement"><Trophy/><div><span>ГЛАВНЫЙ РЕЗУЛЬТАТ</span><strong>Жим гантелей +7,5 кг</strong><small>за последние 8 недель</small></div></div>
    <section className="bar-chart-card"><div className="section-heading"><h2>Рабочий вес</h2><span>кг × 10</span></div><div className="bar-chart" role="img" aria-label="Рабочий вес вырос до 42,5 килограмма за восемь недель">{[28,31,35,41,48,56,65,78].map((v,i)=><i key={i}><em style={{height:v+"%"}}/><span>{i+1}</span></i>)}</div><div className="chart-result"><strong>42,5 кг</strong><span>+21% за период</span></div></section>
    <div className="insight-card"><Bot/><div><span>AI-ВЫВОД</span><b>Грудь прогрессирует, спина отстаёт</b><p>В горизонтальных тягах вес не менялся три недели. Предлагаю обновить прогрессию.</p></div><button onClick={() => navigate("coach")}>Посмотреть изменение</button></div>
    <div className="records-list"><div><span>Личные рекорды</span><strong>6</strong></div><div><span>Объём за месяц</span><strong>67,4 т</strong></div><div><span>Средний RIR</span><strong>2,1</strong></div></div>
  </div>;

  if (screen === "coach") return <div className="phone-screen coach-screen">
    <Header eyebrow="КОНТЕКСТНЫЙ AI-КОУЧ" title="Спроси по своим данным" onBack={goBack} onMore={more} />
    <div className="context-chips"><span><Zap/>Readiness 78</span><span><CalendarDays/>План недели</span><span><HeartPulse/>2 ограничения</span></div>
    <div className="quick-prompts">{["Почему сегодня веса ниже?","Замени упражнение — болит плечо","У меня осталось 30 минут"].map((prompt)=><button key={prompt} onClick={() => setCoachInput(prompt)}>{prompt}</button>)}</div>
    <div className="chat-thread"><div className="bubble user">Замени сведение рук в тренажёре — плечу больно.</div><div className="bubble ai"><Bot/><div><p>Уберу движение, которое провоцирует боль. Подойдёт <b>жим гантелей лёжа на полу нейтральным хватом</b>: амплитуда короче, плечо стабильнее.</p><div className="change-diff"><span>БЫЛО</span><s>Сведение рук в тренажёре</s><span>СТАНЕТ</span><b>Жим гантелей на полу</b><small>3 × 10 · запас 3 повтора</small></div><p className="reason"><ShieldCheck/> Учтено ограничение левого плеча</p><div className="diff-actions"><Button variant="outline" onClick={() => setCoachInput("Предложи другую безопасную замену")}>Изменить</Button><Button onClick={() => announce("Замена принята и добавлена в тренировку")}>Принять замену</Button></div></div></div></div>
    <div className="chat-input"><button type="button" aria-label="Добавить вложение" onClick={() => announce("Выберите фото или документ")}><Plus aria-hidden="true"/></button><label className="sr-only" htmlFor="coach-message">Сообщение AI-коучу</label><input id="coach-message" value={coachInput} onChange={(event) => setCoachInput(event.target.value)} placeholder="Напиши или скажи…"/><button type="button" aria-label="Отправить сообщение" className="send" onClick={() => { if(coachInput.trim()){ announce("Сообщение отправлено AI-коучу"); setCoachInput(""); } }}><ArrowRight aria-hidden="true"/></button></div>
  </div>;

  if (screen === "profile") return <div className="phone-screen has-nav">
    <Header eyebrow="АККАУНТ" title="Профиль" onMore={more} />
    <div className="profile-card"><div className="avatar">НМ</div><div><b>Никита Михайлов</b><span>Мультиспорт · уровень 18</span></div><ChevronRight/></div>
    <div className="profile-goal"><span>ГЛАВНАЯ ЦЕЛЬ</span><div><b>Стать сильнее</b><strong>68%</strong></div><Progress value={68}/><small>До контрольной точки — 5 недель</small></div>
    <div className="settings-list"><button><Trophy/><span><b>Цели и уровни</b><small>4 направления спорта</small></span><ChevronRight/></button><button><CalendarDays/><span><b>Расписание и места</b><small>4 тренировочных дня</small></span><ChevronRight/></button><button><HeartPulse/><span><b>Ограничения</b><small>Левое плечо · правое колено</small></span><ChevronRight/></button><button onClick={()=>navigate("devices")}><Watch/><span><b>Устройства</b><small>Xiaomi Watch S3 · подключено</small></span><ChevronRight/></button><button><Settings/><span><b>Настройки</b><small>Единицы, тема, уведомления</small></span><ChevronRight/></button></div>
    <button className="pro-banner" onClick={()=>navigate("subscription")}><Sparkles/><div><b>Athletica Pro</b><span>Адаптация всех видов спорта</span></div><ChevronRight/></button>
    <BottomNav active="profile" navigate={navigate}/>
  </div>;

  if (screen === "devices") return <div className="phone-screen">
    <Header eyebrow="ДАННЫЕ И СИНХРОНИЗАЦИЯ" title="Устройства" onBack={goBack} onMore={more} />
    <div className="device-card"><span className="watch-icon"><Watch/></span><div><b>Xiaomi Watch S3</b><span>Mi Fitness → Health Connect</span><small><i/> Синхронизировано 2 мин назад</small></div><ChevronRight/></div>
    <section className="data-quality"><div><span>КАЧЕСТВО ДАННЫХ</span><strong>86%</strong></div><Progress value={86}/><p>Данных достаточно для уверенной адаптации плана.</p></section>
    <div className="metric-grid"><Metric label="Сон" value="6:49" detail="сегодня"/><Metric label="Шаги" value="10 420"/><Metric label="Пульс" value="54" detail="в покое"/><Metric label="HRV" value="48 мс"/></div>
    <h2 className="section-title">Источники</h2>
    <div className="source-list"><button><span className="source-icon health"><HeartPulse/></span><div><b>Health Connect</b><span>Сон, пульс, активность</span></div><span className="connected">Подключено</span></button><button><span className="source-icon"><Activity/></span><div><b>Strava</b><span>Велосипед и маршруты</span></div><Plus/></button><button><span className="source-icon"><Waves/></span><div><b>Данные бассейна</b><span>Ручной ввод</span></div><ChevronRight/></button></div>
    <Button className="secondary-action" onClick={() => announce("Выберите источник данных для подключения")}><Plus/> Подключить источник</Button>
  </div>;

  if (screen === "subscription") return <div className="phone-screen subscription-screen">
    <div className="step-header"><button type="button" className="icon-button" aria-label="Вернуться назад" onClick={goBack}><ArrowLeft aria-hidden="true"/></button><span className="pro-pill">PRO</span></div>
    <div className="pro-head"><Sparkles/><p className="eyebrow">ATHLETICA PRO</p><h1>Весь спорт.<br/>Одна логика.</h1><p>AI балансирует нагрузку между залом, бассейном, велосипедом и площадкой.</p></div>
    <div className="benefits"><div><Check/><span><b>Ежедневная адаптация</b><small>по сну, нагрузке и самочувствию</small></span></div><div><Check/><span><b>Неограниченный AI-коуч</b><small>с объяснением каждого изменения</small></span></div><div><Check/><span><b>Полная аналитика</b><small>по каждому виду спорта</small></span></div><div><Check/><span><b>Импорт упражнений по ссылке</b><small>с созданием черновика карточки</small></span></div></div>
    <div className="price-options"><button type="button" aria-pressed={period==="month"} className={period==="month"?"selected":""} onClick={() => setPeriod("month")}><span>1 месяц</span><b>1 290 ₽</b><small>в месяц</small></button><button type="button" aria-pressed={period==="year"} className={period==="year"?"selected":""} onClick={() => setPeriod("year")}><em>−42%</em><span>1 год</span><b>8 990 ₽</b><small>749 ₽ в месяц</small></button></div>
    <Button className="primary-action" onClick={() => announce(`Пробный период активирован: тариф на ${period==="year"?"год":"месяц"}`)}>Попробовать 7 дней бесплатно</Button>
    <p className="legal">Отмена в любой момент. Базовая энциклопедия и техника безопасности остаются бесплатными.</p>
  </div>;

  return <div className="phone-screen"><Header eyebrow="ЭКРАН" title="В разработке" onBack={goBack} onMore={more} /><p className="lead">Этот экран входит в полную карту продукта.</p></div>;
}

export default function Home() {
  const [screen, setScreen] = useState<ScreenId>("today");
  const [, setHistory] = useState<ScreenId[]>([]);
  const [message, setMessage] = useState("");
  const currentIndex = useMemo(() => flatScreens.findIndex((item) => item.id === screen), [screen]);
  const current = flatScreens[currentIndex];
  const goTo = (id: ScreenId) => {
    if (id === screen) return;
    setHistory((items) => [...items, screen]);
    setScreen(id);
    setMessage("");
  };
  const goBack = () => {
    setHistory((items) => {
      const target = items.at(-1) ?? "today";
      setScreen(target);
      return items.slice(0, -1);
    });
    setMessage("");
  };
  const selectScreen = (id: ScreenId) => { setScreen(id); setHistory([]); setMessage(""); };
  const previous = () => selectScreen(flatScreens[(currentIndex - 1 + flatScreens.length) % flatScreens.length].id);
  const next = () => selectScreen(flatScreens[(currentIndex + 1) % flatScreens.length].id);

  return <SidebarProvider defaultOpen style={{"--sidebar-width":"17.5rem"} as CSSProperties}>
    <Sidebar variant="inset" className="prototype-sidebar">
      <SidebarHeader className="sidebar-brand">
        <div className="brand-lockup"><span><Activity/></span><div><b>ATHLETICA</b><small>AI Fitness OS</small></div></div>
        <p>Интерактивная карта продукта</p>
      </SidebarHeader>
      <SidebarContent>
        {groups.map((group) => {
          const GroupIcon = group.icon;
          return <SidebarGroup key={group.label}>
            <SidebarGroupLabel><GroupIcon/>{group.label}</SidebarGroupLabel>
            <SidebarGroupContent><SidebarMenu>{group.items.map((item) => <SidebarMenuItem key={item.id}>
              <SidebarMenuButton isActive={screen===item.id} onClick={()=>selectScreen(item.id)}><span className="screen-number">{String(flatScreens.findIndex(x=>x.id===item.id)+1).padStart(2,"0")}</span><span>{item.label}</span></SidebarMenuButton>
            </SidebarMenuItem>)}</SidebarMenu></SidebarGroupContent>
          </SidebarGroup>;
        })}
      </SidebarContent>
      <SidebarFooter><div className="sidebar-note"><ShieldCheck/><p><b>26 ключевых экранов</b><span>MVP + основа V1</span></p></div></SidebarFooter>
    </Sidebar>
    <SidebarInset className="prototype-main">
      <header className="prototype-topbar">
        <div><SidebarTrigger/><div><span>{currentIndex+1} / {flatScreens.length}</span><h2>{current?.label}</h2></div></div>
        <div className="topbar-actions"><span className="direction-tag">Performance Editorial</span><Button variant="outline" size="icon" onClick={previous} aria-label="Предыдущий экран"><ArrowLeft/></Button><Button variant="outline" size="icon" onClick={next} aria-label="Следующий экран"><ArrowRight/></Button></div>
      </header>
      <main className="prototype-stage">
        <div className="stage-copy"><span>КЛИКАБЕЛЬНЫЙ ПРОТОТИП</span><h1>Все ключевые<br/>экраны приложения</h1><p>Выбирай экран слева или листай стрелками. Внутри телефона работают основные переходы сценария.</p><div className="palette"><i/><i/><i/><i/><span>единый визуальный язык</span></div></div>
        <div className={`phone-shell${screen==="subscription"?" dark-phone":""}`}>
          <div className="phone-status"><span>9:41</span><span><i/><i/><i/></span></div>
          <div className="phone-content" role="region" aria-label={`Предпросмотр приложения: ${current?.label}`}><ScreenCanvas screen={screen} navigate={goTo} goBack={goBack} announce={setMessage}/></div>
          <span className="sr-only" aria-live="polite">Открыт экран: {current?.label}</span>
          {message && <div className="prototype-toast" role="status"><span>{message}</span><button type="button" aria-label="Закрыть уведомление" onClick={() => setMessage("")}>×</button></div>}
          <div className="home-indicator"/>
        </div>
        <aside className="screen-notes"><span>ПРИНЦИП ЭКРАНА</span><h3>{current?.label}</h3><p>{screen==="today"?"Одно главное действие: увидеть лучший выбор на сегодня и сразу начать.":screen==="exercise"||screen==="technique"?"Единый мастер-шаблон: назначение, фазы, мышцы, ошибки, ограничения и прогрессии.":screen==="coach"?"AI показывает изменение как «было → станет» и не меняет план без подтверждения.":"Каждый экран отвечает на один главный вопрос и сохраняет контекст общей нагрузки."}</p><div className="spec-row"><span>Сетка</span><b>4 pt / 20 px</b></div><div className="spec-row"><span>Touch target</span><b>≥ 44 px</b></div><div className="spec-row"><span>Контраст</span><b>AA</b></div></aside>
      </main>
    </SidebarInset>
  </SidebarProvider>;
}
