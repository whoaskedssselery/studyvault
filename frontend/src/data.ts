import type { JSONContent } from "@tiptap/react";

export type User = {
  id: string;
  name: string;
  initials: string;
  faculty: string;
  color: string;
  email?: string;
};
export type Attachment = { name: string; size: number; dataUrl: string };
export type Note = {
  id: string;
  title: string;
  description: string;
  subject: string;
  type: "Конспект" | "Шпаргалка";
  semester: number;
  tags: string[];
  authorId: string;
  createdAt: string;
  content: JSONContent;
  text: string;
  status: "published" | "pending" | "rejected" | "draft";
  rejection?: string;
  ratings: Record<string, number>;
  saves: string[];
  comments: { id: string; userId: string; text: string; createdAt: string }[];
  attachments: Attachment[];
  views: number;
};
export type Database = { version: 1; users: User[]; notes: Note[] };
export const subjects = [
  "Все предметы",
  "Математика",
  "Программирование",
  "Базы данных",
  "Физика",
  "Английский язык",
  "История",
];
export const subjectColors: Record<string, string> = {
  Математика: "purple",
  Программирование: "blue",
  "Базы данных": "orange",
  Физика: "green",
  "Английский язык": "pink",
  История: "orange",
};
export const demoUser: User = {
  id: "demo",
  name: "Алексей Смирнов",
  initials: "АС",
  faculty: "Информационные технологии · 2 курс",
  color: "purple",
  email: "alexey@example.test",
};
const users: User[] = [
  demoUser,
  {
    id: "anna",
    name: "Анна Ковалева",
    initials: "АК",
    faculty: "Прикладная математика · 3 курс",
    color: "pink",
  },
  {
    id: "max",
    name: "Максим Волков",
    initials: "МВ",
    faculty: "Информационные технологии · 2 курс",
    color: "blue",
  },
  {
    id: "daria",
    name: "Дарья Соколова",
    initials: "ДС",
    faculty: "Физический факультет · 2 курс",
    color: "green",
  },
  {
    id: "ivan",
    name: "Иван Петров",
    initials: "ИП",
    faculty: "Информационные технологии · 3 курс",
    color: "orange",
  },
];
const p = (text: string): JSONContent => ({
  type: "paragraph",
  content: [{ type: "text", text }],
});
const h = (text: string): JSONContent => ({
  type: "heading",
  attrs: { level: 2 },
  content: [{ type: "text", text }],
});
const doc = (title: string, intro: string): JSONContent => ({
  type: "doc",
  content: [
    p(intro),
    h("Самое важное"),
    p(
      "Начните с основных определений, затем разберите примеры. Конспект помогает повторить материал перед семинаром и подготовиться к экзамену.",
    ),
    h("Разбор темы"),
    p(
      title === "Производные: от определения к практике"
        ? "Производная функции — это предел отношения приращения функции к приращению аргумента. Геометрически производная равна угловому коэффициенту касательной к графику в выбранной точке."
        : "Теория становится понятнее, когда вы применяете её на практике. Выпишите ключевые понятия и проверьте, можете ли объяснить каждое из них своими словами.",
    ),
    {
      type: "blockquote",
      content: [
        p(
          title === "Производные: от определения к практике"
            ? "f′(x) = lim [f(x + h) − f(x)] / h, при h → 0"
            : "Хороший конспект — это не копия учебника, а понятная структура знаний.",
        ),
      ],
    },
    h("Примеры и правила"),
    {
      type: "bulletList",
      content: [
        "Разберите определение и условия его применения.",
        "Решите простой пример самостоятельно.",
        "Проверьте ответ и запишите типичные ошибки.",
      ].map((text) => ({ type: "listItem", content: [p(text)] })),
    },
    h("Проверьте себя"),
    p(
      "Какие основные понятия вы запомнили? Где можно применить этот материал? Обсудите сложные моменты в комментариях — вместе разобраться проще.",
    ),
  ],
});
const list = (items: string[]): JSONContent => ({
  type: "bulletList",
  content: items.map((text) => ({ type: "listItem", content: [p(text)] })),
});
const quote = (text: string): JSONContent => ({
  type: "blockquote",
  content: [p(text)],
});
const code = (text: string): JSONContent => ({
  type: "codeBlock",
  content: [{ type: "text", text }],
});
function seedContent(index: number, title: string, intro: string): JSONContent {
  const sections: JSONContent[][] = [
    [
      h("Что такое производная"),
      p(
        "Производная показывает, как быстро изменяется функция в выбранной точке. Геометрически это угловой коэффициент касательной к графику.",
      ),
      quote("f′(x) = lim [f(x + h) − f(x)] / h, при h → 0"),
      h("Основные правила"),
      list([
        "(C)′ = 0 — производная постоянной равна нулю.",
        "(xⁿ)′ = n · xⁿ⁻¹ — правило для степенной функции.",
        "(f + g)′ = f′ + g′ — сумму дифференцируем по частям.",
        "(f · g)′ = f′ · g + f · g′ — правило произведения.",
      ]),
      h("Разбираем пример"),
      p(
        "Пусть f(x) = 3x² + 2x − 7. По правилу суммы: f′(x) = 6x + 2. В точке x = 1 получаем f′(1) = 8 — это наклон касательной.",
      ),
      h("Частая ошибка"),
      p(
        "При дифференцировании сложной функции не забывайте про внутреннюю производную. Например, (sin(2x))′ = 2cos(2x), а не просто cos(2x).",
      ),
      h("Проверьте себя"),
      p(
        "Найдите производную f(x) = x³ − 4x. Ответ: f′(x) = 3x² − 4. Попробуйте объяснить каждое действие одногруппнику.",
      ),
    ],
    [
      h("Соединяем таблицы"),
      list([
        "INNER JOIN возвращает строки, для которых нашлась пара в обеих таблицах.",
        "LEFT JOIN сохраняет все строки левой таблицы. Если пары нет, поля правой таблицы будут NULL.",
      ]),
      code(
        "SELECT u.display_name, n.title\nFROM users AS u\nLEFT JOIN notes AS n ON n.user_id = u.id;",
      ),
      h("Группировка и агрегаты"),
      p(
        "GROUP BY объединяет строки в группы. COUNT, SUM и AVG вычисляют значения для каждой группы. WHERE фильтрует исходные строки, HAVING — результат группировки.",
      ),
      code(
        "SELECT user_id, COUNT(*) AS note_count\nFROM notes\nGROUP BY user_id\nHAVING COUNT(*) >= 3;",
      ),
      h("Подзапрос"),
      p(
        "Подзапрос — запрос внутри другого SQL-запроса. Например, можно выбрать авторов, которые создали хотя бы один конспект.",
      ),
      code(
        "SELECT display_name FROM users\nWHERE id IN (SELECT user_id FROM notes);",
      ),
    ],
    [
      h("Что значит сложность"),
      p(
        "Сложность описывает рост числа операций при увеличении размера входных данных. Для сортировок важно различать худший и средний случаи.",
      ),
      list([
        "Пузырьковая сортировка: O(n²) в среднем и в худшем случае.",
        "Сортировка слиянием: O(n log n), но требует дополнительной памяти.",
        "Быстрая сортировка: в среднем O(n log n), в худшем случае O(n²).",
      ]),
      h("Сортировка слиянием на Python"),
      code(
        "def merge_sort(items):\n    if len(items) <= 1:\n        return items\n    middle = len(items) // 2\n    left = merge_sort(items[:middle])\n    right = merge_sort(items[middle:])\n    result = []\n    i = j = 0\n    while i < len(left) and j < len(right):\n        if left[i] <= right[j]:\n            result.append(left[i]); i += 1\n        else:\n            result.append(right[j]); j += 1\n    return result + left[i:] + right[j:]",
      ),
      h("Проверьте себя"),
      p(
        "Почему деление массива пополам даёт логарифмическую глубину? Чем стабильная сортировка отличается от нестабильной?",
      ),
    ],
    [
      h("Идеальный газ"),
      quote("pV = νRT"),
      p(
        "p — давление, V — объём, ν — количество вещества, R — универсальная газовая постоянная, T — абсолютная температура в кельвинах.",
      ),
      h("Первое начало термодинамики"),
      quote("Q = ΔU + A"),
      p(
        "Здесь A — работа, совершённая газом. Подведённая теплота расходуется на изменение внутренней энергии и выполнение работы.",
      ),
      h("Основные процессы"),
      list([
        "Изохорный: объём постоянен, работа газа равна нулю.",
        "Изобарный: давление постоянно, A = p · ΔV.",
        "Изотермический: температура постоянна, для идеального газа ΔU = 0.",
        "Адиабатный: нет теплообмена с окружающей средой, Q = 0.",
      ]),
      h("Второе начало"),
      p(
        "Теплота не переходит самопроизвольно от холодного тела к горячему. В циклическом процессе нельзя полностью превратить полученную теплоту в работу без других изменений.",
      ),
    ],
    [
      h("Present: настоящее"),
      list([
        "Present Simple — привычки и общие факты: I study every day.",
        "Present Continuous — процесс сейчас: I am studying now.",
        "Present Perfect — результат к настоящему моменту: I have finished my notes.",
        "Present Perfect Continuous — длительность до настоящего момента: I have been studying for two hours.",
      ]),
      h("Past: прошлое"),
      list([
        "Past Simple — завершённое событие: I studied yesterday.",
        "Past Continuous — процесс в момент в прошлом: I was studying at 6 pm.",
        "Past Perfect — действие до другого прошлого события: I had finished before they arrived.",
      ]),
      h("Future: будущее"),
      list([
        "Future Simple — решение или прогноз: I will study tomorrow.",
        "Future Continuous — процесс в будущем: I will be studying at 6 pm.",
        "Future Perfect — завершение к сроку: I will have finished by Friday.",
      ]),
      quote(
        "Сначала определите смысл: факт, процесс, результат или длительность. Затем выберите временную точку.",
      ),
    ],
    [
      h("Четыре принципа"),
      list([
        "Инкапсуляция — скрываем внутреннюю реализацию и открываем понятный интерфейс.",
        "Абстракция — выделяем существенные свойства объекта.",
        "Наследование — строим новый класс на основе существующего.",
        "Полиморфизм — используем один интерфейс для объектов разных типов.",
      ]),
      h("Простой пример на Java"),
      code(
        "interface Shape {\n    double area();\n}\n\nclass Square implements Shape {\n    private final double side;\n    Square(double side) { this.side = side; }\n    public double area() { return side * side; }\n}",
      ),
      p(
        "Код, который работает с Shape, может вызывать area(), не зная конкретного типа фигуры. Это пример полиморфизма через интерфейс.",
      ),
      h("Проверьте себя"),
      p(
        "Почему поле side закрыто? Можно ли добавить Circle без изменения кода, работающего с Shape?",
      ),
    ],
    [
      h("Матрица и её размер"),
      p(
        "Матрица — прямоугольная таблица чисел. Матрица размера m × n содержит m строк и n столбцов.",
      ),
      list([
        "Складывать можно матрицы одинакового размера.",
        "При умножении A · B число столбцов A должно совпадать с числом строк B.",
        "В общем случае A · B не равно B · A.",
      ]),
      h("Определитель 2 × 2"),
      quote("det [[a, b], [c, d]] = ad − bc"),
      p(
        "Например, для матрицы [[2, 1], [3, 4]] определитель равен 2 · 4 − 1 · 3 = 5.",
      ),
      h("Метод Гаусса"),
      p(
        "Приведите систему к ступенчатому виду с помощью элементарных преобразований строк, затем найдите неизвестные обратной подстановкой.",
      ),
      h("Проверьте себя"),
      p(
        "Может ли прямоугольная матрица иметь определитель? Что означает нулевой определитель квадратной матрицы для её обратимости?",
      ),
    ],
    [
      h("Зачем нужна нормализация"),
      p(
        "Нормализация помогает уменьшить избыточность и избежать аномалий вставки, изменения и удаления. Основа анализа — ключи и функциональные зависимости.",
      ),
      list([
        "1НФ: в каждой ячейке одно значение, нет повторяющихся групп.",
        "2НФ: таблица в 1НФ; неключевые атрибуты не зависят от части составного кандидатного ключа.",
        "3НФ: для каждой нетривиальной зависимости X → A либо X — суперключ, либо A входит в кандидатный ключ.",
      ]),
      h("Практический пример"),
      p(
        "В таблице записей на курсы (student_id, course_id, student_name, course_title) ключ составной. Имя зависит только от student_id, название курса — только от course_id. Это частичные зависимости.",
      ),
      quote(
        "Разделите данные на students, courses и enrollments. Свяжите таблицы внешними ключами.",
      ),
      h("Проверьте себя"),
      p(
        "Какие данные придётся менять при переименовании курса в исходной таблице? Как нормализация устраняет эту проблему?",
      ),
    ],
  ];
  return {
    type: "doc",
    content: [
      p(intro),
      ...(sections[index] || doc(title, intro).content || []),
    ],
  };
}
function contentText(node: JSONContent): string {
  return node.text || (node.content || []).map(contentText).join(" ");
}
const examples = [
  [
    "Производные: от определения к практике",
    "Все правила дифференцирования, понятные примеры и разбор типичных ошибок. То, что нужно перед контрольной.",
    "Математика",
    "Конспект",
    "anna",
    "производные,матан,2 семестр",
    4.9,
    128,
  ],
  [
    "SQL без паники: JOIN, GROUP BY и подзапросы",
    "Наглядная шпаргалка по SQL с примерами запросов. Разбираемся, чем LEFT JOIN отличается от INNER JOIN.",
    "Базы данных",
    "Шпаргалка",
    "max",
    "SQL,базы данных,экзамен",
    4.8,
    96,
  ],
  [
    "Алгоритмы сортировки: сравнение и сложность",
    "От пузырька до быстрой сортировки. Принципы работы, примеры на Python и таблица сложности алгоритмов.",
    "Программирование",
    "Конспект",
    "ivan",
    "алгоритмы,Python,структуры данных",
    4.9,
    84,
  ],
  [
    "Термодинамика: главное в одном конспекте",
    "Первое и второе начала термодинамики, идеальный газ и основные формулы с пояснениями.",
    "Физика",
    "Конспект",
    "daria",
    "термодинамика,формулы,физика",
    4.7,
    67,
  ],
  [
    "Времена английского языка — на одной странице",
    "Двенадцать времён, слова-маркеры и примеры. Компактная таблица для быстрого повторения.",
    "Английский язык",
    "Шпаргалка",
    "anna",
    "грамматика,English,таблица",
    4.8,
    112,
  ],
  [
    "ООП: классы, наследование и полиморфизм",
    "Четыре принципа объектно-ориентированного программирования на простых примерах из жизни и кода.",
    "Программирование",
    "Конспект",
    "demo",
    "ООП,Java,программирование",
    4.6,
    53,
  ],
  [
    "Линейная алгебра: матрицы и определители",
    "Операции с матрицами, метод Гаусса и вычисление определителей второго и третьего порядка.",
    "Математика",
    "Конспект",
    "max",
    "матрицы,линейная алгебра",
    4.8,
    75,
  ],
  [
    "Нормализация баз данных: 1НФ, 2НФ, 3НФ",
    "Как избавиться от дублирования данных и спроектировать понятную схему. Примеры пошаговой нормализации.",
    "Базы данных",
    "Конспект",
    "demo",
    "нормализация,проектирование,SQL",
    4.9,
    42,
  ],
] as const;
export function initialDatabase(): Database {
  const notes: Note[] = examples.map((e, i) => ({
    id: `note-${i + 1}`,
    title: e[0],
    description: e[1],
    subject: e[2],
    type: e[3],
    authorId: e[4],
    semester: 2,
    tags: e[5].split(","),
    createdAt: new Date(Date.UTC(2026, 9, 8 - i)).toISOString(),
    content: seedContent(i, e[0], e[1]),
    text: `${e[0]} ${e[1]} ${contentText(seedContent(i, e[0], e[1]))}`,
    status: "published",
    ratings: { seed1: e[6], seed2: e[6] },
    saves: i === 0 || i === 2 ? ["demo"] : [],
    comments:
      i === 0
        ? [
            {
              id: "c1",
              userId: "max",
              text: "Наконец-то понял геометрический смысл производной. Спасибо за понятное объяснение!",
              createdAt: "2026-10-08T12:00:00Z",
            },
            {
              id: "c2",
              userId: "demo",
              text: "Очень пригодилось для подготовки к семинару. Добавил в избранное 🙌",
              createdAt: "2026-10-08T14:00:00Z",
            },
          ]
        : [],
    attachments: [],
    views: e[7],
  }));
  notes.push(
    ...[9, 10].map((n, i) => {
      const title = i
        ? "Интегралы: основные методы решения"
        : "Графы и поиск в ширину";
      const description = i
        ? "Замена переменной и интегрирование по частям с примерами."
        : "Очередь, посещённые вершины и кратчайший путь в невзвешенном графе.";
      return {
        ...notes[i],
        id: `note-${n}`,
        title,
        description,
        content: {
          type: "doc",
          content: [
            p(description),
            h(i ? "Интегрирование по частям" : "Как работает BFS"),
            p(
              i
                ? "Формула: ∫u dv = uv − ∫v du. Выбирайте u так, чтобы его производная стала проще."
                : "Поместите стартовую вершину в очередь и отметьте её как посещённую. Затем по очереди обрабатывайте вершины и добавляйте в очередь непосещённых соседей.",
            ),
            quote(
              i
                ? "∫x cos(x) dx = x sin(x) + cos(x) + C"
                : "Сложность BFS при хранении графа списками смежности: O(V + E).",
            ),
          ],
        },
        text: `${title} ${description}`,
        type: "Конспект" as const,
        tags: i ? ["интегралы", "матан"] : ["графы", "алгоритмы"],
        status: "pending" as const,
        authorId: i ? "anna" : "ivan",
        subject: i ? "Математика" : "Программирование",
        ratings: {},
        saves: [],
        comments: [],
        createdAt: "2026-10-09T09:00:00Z",
      };
    }),
  );
  return { version: 1, users, notes };
}
export const storageKey = "studyvault.database.v1";
export const sessionKey = "studyvault.session.v1";
export const repository = {
  async read(): Promise<Database> {
    const raw = localStorage.getItem(storageKey);
    if (raw) {
      try {
        const value = JSON.parse(raw);
        if (
          value.version === 1 &&
          Array.isArray(value.notes) &&
          Array.isArray(value.users)
        )
          return value;
      } catch {
        /* Recover invalid demo storage. */
      }
    }
    return initialDatabase();
  },
  async write(db: Database) {
    localStorage.setItem(storageKey, JSON.stringify(db));
    return db;
  },
};
export function average(note: Note) {
  const values = Object.values(note.ratings);
  return values.length ? values.reduce((a, b) => a + b, 0) / values.length : 0;
}
export function formatDate(date: string) {
  return new Intl.DateTimeFormat("ru", {
    day: "numeric",
    month: "short",
  }).format(new Date(date));
}
