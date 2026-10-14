// Команда seed наполняет локальную базу демонстрационными данными: модератором, несколькими студентами,
// общими предметами и материалами во всех статусах с комментариями, оценками и избранным.
// Нужна для показа и скриншотов; в продакшене не запускается. Повторный запуск ничего не делает.
//
//	cd backend && go run ./cmd/seed
//
// Пароль всех демо-аккаунтов берётся из SEED_PASSWORD (не короче 8 символов). Если переменной нет, пароль
// генерируется и печатается один раз в конце.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"

	"github.com/whoaskedssselery/studyvault/backend/internal/auth"
	"github.com/whoaskedssselery/studyvault/backend/internal/config"
	"github.com/whoaskedssselery/studyvault/backend/internal/db"
	"github.com/whoaskedssselery/studyvault/backend/internal/library"
	"github.com/whoaskedssselery/studyvault/backend/internal/note"
	"github.com/whoaskedssselery/studyvault/backend/internal/search"
	"github.com/whoaskedssselery/studyvault/backend/internal/tag"
)

const (
	moderatorEmail = "moderator@example.com"
	demoEmail      = "demo@example.com"
)

func main() {
	if err := run(); err != nil {
		slog.Error("seed failed", "err", err)
		os.Exit(1)
	}
}

type person struct {
	email, name, faculty string
	id                   uuid.UUID
}

type material struct {
	author   string // ключ в people
	subject  string
	title    string
	kind     string
	semester int16
	summary  string
	paras    []string
	tags     []string
	outcome  string // published, pending, rejected, draft
	reason   string // причина отказа
	daysAgo  int
	views    int
}

func run() error {
	_ = godotenv.Load("../.env")
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	q := db.New(pool)

	if _, err := q.GetUserByEmail(ctx, moderatorEmail); err == nil {
		fmt.Println("the demo data is already there: nothing to do")
		return nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}

	password := os.Getenv("SEED_PASSWORD")
	generated := false
	if password == "" {
		b := make([]byte, 9)
		_, _ = rand.Read(b)
		password, generated = hex.EncodeToString(b), true
	}
	if len(password) < 8 {
		return errors.New("SEED_PASSWORD must be at least 8 characters")
	}

	noteOpts := []note.Option{}
	libOpts := []library.Option{}
	var meili *search.Meili
	if cfg.MeiliURL != "" {
		meili = search.NewMeili(cfg.MeiliURL, cfg.MeiliKey, search.IndexUID)
		noteOpts = append(noteOpts, note.WithIndexer(meili))
		libOpts = append(libOpts, library.WithIndexer(meili))
	}
	authSvc := auth.NewService(q, cfg.JWTSecret)
	notes := note.NewService(q, noteOpts...)
	lib := library.NewService(q, libOpts...)
	tags := tag.NewService(q)

	people := map[string]*person{
		"mod":    {email: moderatorEmail, name: "Елена Модераторова", faculty: "Учебный отдел"},
		"demo":   {email: demoEmail, name: "Алексей Смирнов", faculty: "Информационные технологии · 2 курс"},
		"maria":  {email: "maria@example.com", name: "Мария Иванова", faculty: "Математика и механика"},
		"dmitry": {email: "dmitry@example.com", name: "Дмитрий Орлов", faculty: "Информационные технологии · 3 курс"},
		"anna":   {email: "anna@example.com", name: "Анна Петрова", faculty: "Прикладная информатика"},
	}
	for _, key := range []string{"mod", "demo", "maria", "dmitry", "anna"} {
		p := people[key]
		res, err := authSvc.Register(ctx, p.email, password, p.name, "seed")
		if err != nil {
			return fmt.Errorf("register %s: %w", p.email, err)
		}
		p.id = res.User.ID
		if _, err := authSvc.UpdateProfile(ctx, p.id, auth.ProfilePatch{SetFaculty: true, Faculty: &p.faculty}); err != nil {
			return err
		}
	}
	if err := authSvc.SetRole(ctx, moderatorEmail, auth.RoleModerator); err != nil {
		return err
	}
	mod := people["mod"].id

	subjectIDs := map[string]uuid.UUID{}
	for _, s := range []struct{ title, color string }{
		{"Математика", "#7c3aed"}, {"Программирование", "#2563eb"}, {"Базы данных", "#ea580c"},
		{"Физика", "#16a34a"}, {"Английский язык", "#db2777"}, {"История", "#a16207"},
	} {
		color := s.color
		sub, err := lib.CreateSubject(ctx, mod, s.title, &color)
		if err != nil {
			return fmt.Errorf("subject %s: %w", s.title, err)
		}
		subjectIDs[s.title] = sub.ID
	}

	ids := map[string]uuid.UUID{} // title -> id
	for _, m := range materials() {
		author := people[m.author]
		subject := subjectIDs[m.subject]
		var summary *string
		if m.summary != "" {
			summary = &m.summary
		}
		semester := m.semester
		n, err := notes.Create(ctx, author.id, note.Input{
			Title: m.title, Content: document(m.title, m.paras), Kind: m.kind,
			Description: summary, Semester: &semester, LibrarySubjectID: &subject,
		})
		if err != nil {
			return fmt.Errorf("create %q: %w", m.title, err)
		}
		ids[m.title] = n.ID
		if err := attachTags(ctx, tags, author.id, n.ID, m.tags); err != nil {
			return err
		}

		if m.outcome != "draft" {
			if _, err := lib.Submit(ctx, author.id, n.ID); err != nil {
				return fmt.Errorf("submit %q: %w", m.title, err)
			}
		}
		switch m.outcome {
		case "published":
			_, err = lib.Decide(ctx, mod, n.ID, library.DecisionApproved, "")
		case "rejected":
			_, err = lib.Decide(ctx, mod, n.ID, library.DecisionRejected, m.reason)
		}
		if err != nil {
			return fmt.Errorf("decide %q: %w", m.title, err)
		}

		// Разнести материалы по времени и дать им просмотры, чтобы лента и сортировки выглядели живо.
		when := time.Now().AddDate(0, 0, -m.daysAgo)
		if _, err := pool.Exec(ctx, `UPDATE notes SET created_at = $2::timestamptz, updated_at = $2::timestamptz,
			published_at = CASE WHEN status = 'published' THEN $2::timestamptz END, views = $3 WHERE id = $1`, n.ID, when, int64(m.views)); err != nil {
			return err
		}
	}

	// Оценки, комментарии и избранное только у опубликованных материалов, как и разрешает API.
	rate := func(title, who string, v int) error {
		_, err := lib.Rate(ctx, people[who].id, ids[title], v)
		return err
	}
	say := func(title, who, text string) error {
		_, err := lib.AddComment(ctx, people[who].id, ids[title], text)
		return err
	}
	save := func(title, who string) error { return lib.AddBookmark(ctx, people[who].id, ids[title]) }

	for _, step := range []func() error{
		func() error { return rate("Пределы функций", "demo", 5) },
		func() error { return rate("Пределы функций", "dmitry", 4) },
		func() error { return rate("Пределы функций", "anna", 5) },
		func() error { return rate("Производные: таблица", "demo", 5) },
		func() error { return rate("Производные: таблица", "anna", 5) },
		func() error { return rate("Указатели в Go", "maria", 4) },
		func() error { return rate("Указатели в Go", "anna", 5) },
		func() error { return rate("Шпаргалка по Git", "demo", 4) },
		func() error { return rate("Шпаргалка по Git", "maria", 4) },
		func() error { return rate("Нормальные формы", "demo", 5) },
		func() error { return rate("SQL JOIN: шпаргалка", "demo", 5) },
		func() error { return rate("SQL JOIN: шпаргалка", "dmitry", 5) },
		func() error { return rate("SQL JOIN: шпаргалка", "maria", 4) },
		func() error { return rate("Законы Ньютона", "dmitry", 3) },
		func() error { return rate("Irregular verbs", "maria", 4) },

		func() error {
			return say("Пределы функций", "demo", "Очень помогло перед коллоквиумом, спасибо!")
		},
		func() error {
			return say("Пределы функций", "dmitry", "Добавьте, пожалуйста, пример с односторонними пределами.")
		},
		func() error {
			return say("SQL JOIN: шпаргалка", "demo", "Лучшая шпаргалка по JOIN, распечатал.")
		},
		func() error {
			return say("Указатели в Go", "maria", "Наконец понятно, чем указатель отличается от значения.")
		},
		func() error {
			return say("Нормальные формы", "dmitry", "Для третьей нормальной формы хорошо бы ещё один пример.")
		},

		func() error { return save("Пределы функций", "demo") },
		func() error { return save("SQL JOIN: шпаргалка", "demo") },
		func() error { return save("Производные: таблица", "demo") },
		func() error { return save("Указатели в Go", "demo") },
	} {
		if err := step(); err != nil {
			return err
		}
	}

	if meili != nil {
		if err := meili.Wait(ctx); err != nil {
			slog.Warn("the search index was not confirmed; run cmd/reindex", "err", err)
		}
	}

	fmt.Println("demo data created:")
	fmt.Printf("  moderator: %s\n  student:   %s (reads, comments, rates, saves)\n  authors:   maria@, dmitry@, anna@example.com\n", moderatorEmail, demoEmail)
	if generated {
		fmt.Printf("  password of every demo account: %s (shown once; set SEED_PASSWORD to choose your own)\n", password)
	} else {
		fmt.Println("  password of every demo account: the value of SEED_PASSWORD")
	}
	return nil
}

// attachTags создаёт теги автора (если их ещё нет) и привязывает к материалу.
func attachTags(ctx context.Context, svc *tag.Service, author, noteID uuid.UUID, names []string) error {
	if len(names) == 0 {
		return nil
	}
	existing, err := svc.List(ctx, author)
	if err != nil {
		return err
	}
	byName := map[string]uuid.UUID{}
	for _, t := range existing {
		byName[t.Name] = t.ID
	}
	var tagIDs []uuid.UUID
	for _, name := range names {
		id, ok := byName[name]
		if !ok {
			t, err := svc.Create(ctx, author, name)
			if err != nil {
				return fmt.Errorf("create tag %q: %w", name, err)
			}
			id = t.ID
		}
		tagIDs = append(tagIDs, id)
	}
	return svc.SetNoteTags(ctx, author, noteID, tagIDs)
}

// document собирает документ редактора Tiptap: заголовок и абзацы.
func document(title string, paras []string) []byte {
	content := fmt.Sprintf(`{"type":"heading","attrs":{"level":1},"content":[{"type":"text","text":%q}]}`, title)
	for _, p := range paras {
		content += fmt.Sprintf(`,{"type":"paragraph","content":[{"type":"text","text":%q}]}`, p)
	}
	return []byte(`{"type":"doc","content":[` + content + `]}`)
}

func materials() []material {
	return []material{
		{author: "maria", subject: "Математика", title: "Пределы функций", kind: note.KindNote, semester: 1,
			summary: "Определение предела, односторонние пределы и замечательные пределы с примерами.",
			paras: []string{
				"Предел функции в точке показывает, к какому значению стремится функция при приближении аргумента к этой точке.",
				"Первый замечательный предел: sin(x)/x стремится к 1 при x, стремящемся к 0. Он нужен для вычисления многих тригонометрических пределов.",
				"Для раскрытия неопределённости 0/0 помогают разложение на множители, домножение на сопряжённое и правило Лопиталя. См. также [[Производные: таблица]].",
			}, tags: []string{"экзамен", "лекция"}, outcome: "published", daysAgo: 12, views: 214},
		{author: "maria", subject: "Математика", title: "Производные: таблица", kind: note.KindCheatSheet, semester: 1,
			summary: "Таблица производных элементарных функций и правила дифференцирования.",
			paras: []string{
				"Константа: 0. Степень x^n: n·x^(n-1). Экспонента e^x: e^x. Логарифм ln x: 1/x.",
				"Синус: cos x. Косинус: минус sin x. Правила: производная суммы, произведения, частного и сложной функции.",
			}, tags: []string{"шпаргалка", "экзамен"}, outcome: "published", daysAgo: 10, views: 358},
		{author: "maria", subject: "Математика", title: "Интегралы", kind: note.KindNote, semester: 2,
			summary: "Неопределённый и определённый интеграл, замена переменной, интегрирование по частям.",
			paras: []string{
				"Интеграл обратен производной. Основные приёмы: замена переменной и интегрирование по частям.",
				"Формула Ньютона-Лейбница связывает определённый интеграл с первообразной.",
			}, tags: []string{"лекция"}, outcome: "published", daysAgo: 6, views: 97},
		{author: "dmitry", subject: "Программирование", title: "Указатели в Go", kind: note.KindNote, semester: 2,
			summary: "Что такое указатель, когда он нужен и чем отличается передача по значению от передачи по ссылке.",
			paras: []string{
				"Указатель хранит адрес значения. Оператор & берёт адрес, оператор * разыменовывает указатель.",
				"Методы с указателем в получателе могут менять структуру. Нулевой указатель равен nil, его разыменование приводит к панике.",
			}, tags: []string{"go", "лекция"}, outcome: "published", daysAgo: 9, views: 176},
		{author: "dmitry", subject: "Программирование", title: "Шпаргалка по Git", kind: note.KindCheatSheet, semester: 1,
			summary: "Команды, которые нужны каждый день: ветки, коммиты, слияния и откаты.",
			paras: []string{
				"git status, git add, git commit -m. Ветка: git switch -c имя. Слияние: git merge имя.",
				"Откат последнего коммита без потери изменений: git reset --soft HEAD~1. Отменить файл: git restore путь.",
			}, tags: []string{"git", "шпаргалка"}, outcome: "published", daysAgo: 14, views: 421},
		{author: "anna", subject: "Базы данных", title: "Нормальные формы", kind: note.KindNote, semester: 3,
			summary: "Первая, вторая и третья нормальные формы на примере таблицы заказов.",
			paras: []string{
				"Первая нормальная форма: значения атомарны, строки уникальны. Вторая: каждый неключевой атрибут зависит от всего ключа.",
				"Третья: нет транзитивных зависимостей между неключевыми атрибутами. Нормализация убирает дублирование, но увеличивает число соединений.",
			}, tags: []string{"экзамен"}, outcome: "published", daysAgo: 8, views: 132},
		{author: "anna", subject: "Базы данных", title: "SQL JOIN: шпаргалка", kind: note.KindCheatSheet, semester: 3,
			summary: "Все виды соединений таблиц с короткими примерами запросов.",
			paras: []string{
				"INNER JOIN оставляет только совпадающие строки. LEFT JOIN сохраняет все строки левой таблицы, недостающие значения заполняет NULL.",
				"FULL JOIN объединяет обе таблицы целиком. CROSS JOIN даёт декартово произведение. SELF JOIN соединяет таблицу с самой собой.",
			}, tags: []string{"sql", "шпаргалка"}, outcome: "published", daysAgo: 5, views: 503},
		{author: "demo", subject: "Физика", title: "Законы Ньютона", kind: note.KindNote, semester: 1,
			summary: "Три закона Ньютона и типичные задачи на силы.",
			paras: []string{
				"Первый закон: тело сохраняет скорость, если на него не действуют силы. Второй: ускорение пропорционально силе, F = ma.",
				"Третий закон: силы действия и противодействия равны по модулю и противоположны по направлению.",
			}, tags: []string{"лекция"}, outcome: "published", daysAgo: 3, views: 64},
		{author: "demo", subject: "Английский язык", title: "Irregular verbs", kind: note.KindCheatSheet, semester: 1,
			summary: "Неправильные глаголы: три формы, сгруппированные по типам.",
			paras: []string{
				"be, was/were, been. go, went, gone. see, saw, seen. write, wrote, written. take, took, taken.",
				"Глаголы с одинаковыми формами: cut, put, set, hit, let. Сначала выучите эти, затем остальные.",
			}, tags: []string{"шпаргалка"}, outcome: "published", daysAgo: 2, views: 41},
		{author: "dmitry", subject: "Математика", title: "Теория графов: основы", kind: note.KindNote, semester: 4,
			summary: "Вершины, рёбра, степени, пути и связность.",
			paras:   []string{"Граф состоит из вершин и рёбер. Степень вершины равна числу инцидентных рёбер.", "Путь, цикл, связность и деревья как связные графы без циклов."},
			tags:    []string{"лекция"}, outcome: "pending", daysAgo: 1, views: 0},
		{author: "anna", subject: "История", title: "Конспект без источников", kind: note.KindNote, semester: 2,
			summary: "Краткий пересказ главы учебника.",
			paras:   []string{"Текст пересказа без ссылок на источники."},
			outcome: "rejected", reason: "Добавьте ссылки на источники и разберите основные даты: без них материал нельзя опубликовать.", daysAgo: 4, views: 0},
		{author: "demo", subject: "Программирование", title: "Черновик: рекурсия", kind: note.KindNote, semester: 2,
			summary: "", paras: []string{"Заготовка конспекта о рекурсии, допишу позже."}, outcome: "draft", daysAgo: 1, views: 0},
	}
}
