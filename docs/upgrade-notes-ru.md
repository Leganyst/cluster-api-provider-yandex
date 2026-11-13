# Заметки об обновлении CAPY до Cluster API v1.14

Этот документ описывает, что и зачем было изменено в форке
`Leganyst/cluster-api-provider-yandex` при переходе с Cluster API v1.6 на v1.14
и с контракта провайдеров v1beta1 на v1beta2. Он написан для человека, который
сам обновление не делал.

Работа велась в ветке `upgrade/capi-v1.14` по этапам. Каждый этап — отдельный
коммит (или несколько), после которого проект собирается и проходят тесты.

Сокращения: **CAPI** — Cluster API, **CAPY** — этот провайдер, **MGMT** —
управляющий кластер, **WL** — рабочий (workload) кластер, **CAPD** — эталонный
Docker-провайдер из репозитория Cluster API.

## Этап 0. Исходная точка

Проверено на коммите `65c3d9e` (тег `v0.2.0`), Linux (WSL2, Ubuntu 24.04),
Go 1.22.2 — эта версия записана в `go.mod` как `toolchain`.

| Проверка | Результат |
|---|---|
| `go build ./...` | собирается |
| `make manifests generate` | проходит, сгенерированные файлы совпадают с лежащими в репозитории |
| `golangci-lint v1.63.4 run ./...` | замечаний нет |
| `make test` | **падает**, см. ниже |
| `go test ./...` с вручную подготовленным envtest 1.25.0 | проходит: `api/v1alpha1`, `controllers` (34 сценария), `internal/pkg/cloud/scope` |

### Что было сломано ещё до изменений

1. **`make test` не работает «из коробки».** Makefile ставит
   `setup-envtest@latest`, то есть версию без закрепления. Последняя версия
   этого инструмента (v0.25.2) требует Go не ниже 1.26, а в `go.mod` указан
   Go 1.22:

   ```
   go: sigs.k8s.io/controller-runtime/tools/setup-envtest@latest: ... requires go >= 1.26.0 (running go 1.22.2; GOTOOLCHAIN=go1.22.2)
   make: *** [Makefile:169: .../bin/setup-envtest] Error 1
   ```

   То же самое происходит в CI оригинального репозитория: `actions/setup-go`
   выставляет `GOTOOLCHAIN=local`, поэтому задача `test` там сейчас падает.

2. **Старый `setup-envtest` тоже не работает.** Версии до v0.19 скачивают
   бинарные файлы из хранилища Google (`kubebuilder-tools`), которое закрыто:

   ```
   unable to fetch hash for requested version: ... got status "401 Unauthorized" from GCS
   ```

   Из-за этого `KUBEBUILDER_ASSETS` оказывается пустым, и тесты контроллеров
   падают с `exec: "etcd": executable file not found in $PATH`.

   Тесты удалось запустить только с `setup-envtest@release-0.19`, который берёт
   файлы из выпусков `controller-tools` на GitHub.

3. **Тесты вебхуков ходят в DNS.** `TestYandexCluster_ValidateCreate` идёт
   20 секунд, `TestYandexCluster_ValidateUpdate` — 10 секунд: проверка адреса
   вызывает `net.LookupHost` (подробнее в разделе «Найденные проблемы»).

4. **Файлы на Windows получают окончания строк CRLF.** В репозитории не было
   `.gitattributes`, поэтому при `core.autocrlf=true` Makefile и манифесты
   получают CRLF, и сборка в WSL или контейнере ломается. Добавлен
   `.gitattributes` с `* text=auto eol=lf`.

Исправления пунктов 1 и 2 сделаны на этапе 1 (версии инструментов закреплены),
пункт 3 — в отдельном коммите с исправлениями ошибок.

## Этап 1. Зависимости без изменения поведения

Цель этапа: перейти на версии библиотек из Cluster API v1.14 и ничего не
менять в поведении провайдера. Контракт остаётся старым (v1beta1).

### Версии

Версии Go, controller-runtime и `k8s.io/*` взяты из `go.mod` тега `v1.14.4`
Cluster API, версии инструментов — из его `Makefile`.

| Что | Было | Стало |
|---|---|---|
| Go | 1.22 | 1.26.0 (директива `go` в `go.mod`), сборка проверена на 1.26.11 |
| `sigs.k8s.io/cluster-api` | v1.6.1 | v1.14.4 |
| `sigs.k8s.io/cluster-api/api` | — | v1.14.4 (новый отдельный модуль) |
| `sigs.k8s.io/controller-runtime` | v0.16.3 | v0.24.1 |
| `k8s.io/api`, `apimachinery`, `client-go` | v0.28.4 | v0.36.3 |
| `github.com/yandex-cloud/go-sdk` | 2024-02 | v0.41.0 (см. ниже, почему не v0.47.0) |
| `github.com/yandex-cloud/go-genproto` | 2024-02 | v0.131.0 |
| ginkgo / gomega | v2.17.2 / v1.33.1 | v2.32.0 / v1.42.1 |
| `go.uber.org/mock` (mockgen) | v0.4.0 | v0.6.0 |
| controller-gen | v0.16.5 | v0.21.0 |
| kustomize | v5.3.0 | v5.7.0 |
| setup-envtest | `@latest` | v0.24.1 (закреплён) |
| Kubernetes в envtest | 1.25.0 | 1.36.0 |
| golangci-lint | v1.63.4 | v2.12.2 |

Прыжок с CAPI v1.6 сразу на v1.14 собрался после правки импортов, поэтому
промежуточная остановка на v1.10 не понадобилась.

### Что изменено

**`go.mod`, `go.sum`** — новые версии. В CAPI v1.14 типы API вынесены в
отдельный Go-модуль `sigs.k8s.io/cluster-api/api`
([предложение о реорганизации кода](https://github.com/kubernetes-sigs/cluster-api/blob/main/docs/proposals/20260701-code-organization.md)),
он добавлен в зависимости.

**Пути импорта** (по
[руководству миграции v1.10 → v1.11](https://release-1-11.cluster-api.sigs.k8s.io/developer/providers/migrations/v1.10-to-v1.11)):

| Было | Стало | Где |
|---|---|---|
| `cluster-api/api/v1beta1` | `cluster-api/api/core/v1beta1` | типы API провайдера (условия, `APIEndpoint`) |
| `cluster-api/api/v1beta1` | `cluster-api/api/core/v1beta2` | контроллеры, scope: объекты ядра `Cluster` и `Machine` |
| `cluster-api/errors` | `cluster-api/api/deprecated/errors` | `failureReason` |
| `cluster-api/util/conditions` | `cluster-api/util/deprecated/v1beta1/conditions` | старые условия |
| `cluster-api/util/patch` | `cluster-api/util/deprecated/v1beta1/patch` | patch helper для старых условий |

Объекты ядра (`Cluster`, `Machine`) пришлось читать через v1beta2 уже на этом
этапе: функции `util.GetOwnerCluster`, `util.GetOwnerMachine` и другие в
CAPI v1.14 возвращают только типы v1beta2. Из-за этого:

- `cluster.Spec.Paused` стал `*bool`;
- `spec.infrastructureRef` стал `ContractVersionedObjectReference`
  (поля `apiGroup`, `kind`, `name` вместо `apiVersion`, `kind`, `name`,
  `namespace`). Тесты переписаны под новый вид ссылки.

**Вебхуки** (`api/v1alpha1/*_webhook.go`). Интерфейсы `webhook.Validator` и
`webhook.Defaulter` из controller-runtime удалены. Проверки перенесены в
отдельные типы `YandexClusterValidator`, `YandexMachineValidator`,
`YandexMachineTemplateValidator`, которые реализуют типизированный интерфейс
`admission.Validator[T]`. Сами проверки не изменились, тесты вызывают их
через новые типы.

> **Расхождение с исходными требованиями.** В требованиях сказано перейти на
> `CustomValidator` и `CustomDefaulter`. В controller-runtime v0.24 эти
> интерфейсы уже сами помечены устаревшими («use Validator instead»), поэтому
> взят актуальный типизированный вариант `admission.Validator[T]`.

Пустые методы `Default()` удалены: они только писали строку в журнал, а
конфигурации mutating-вебхука для них в `config/webhook/manifests.yaml` не
было, то есть API-сервер их никогда не вызывал.

`admission.Decoder` стал интерфейсом, поэтому в двух вебхуках
(`yandexcluster_mutatingwebhook.go`, `yandexidentity_mutatingwebhook.go`)
поле `*admission.Decoder` заменено на `admission.Decoder`.

**Регистрация типов** (`api/v1alpha1/groupversion_info.go`).
`scheme.Builder` из controller-runtime помечен устаревшим; заменён на
стандартный `runtime.NewSchemeBuilder`, как в CAPD.

**Настройка контроллеров** (`controllers/*_controller.go`):

- предикаты CAPI теперь принимают `*runtime.Scheme` первым аргументом;
- `source.Kind` и `controller.Watch` стали обобщёнными, отдельные вызовы
  `c.Watch(source.Kind(...))` заменены на `Watches(...)` в построителе;
- `ClusterUnpausedAndInfrastructureReady` переименован в
  `ClusterUnpausedAndInfrastructureProvisioned`.

**Тесты** (`controllers/suite_test.go`). CRD ядра для envtest берутся из
каталога модуля Cluster API. Раньше он находился через пакет
`sigs.k8s.io/cluster-api`, но корень модуля больше не является Go-пакетом, а
CRD переехали из `config/crd/bases` в `core/config/crd/bases`. Старый код при
этом падал с разыменованием nil; теперь путь ищется через пакет
`sigs.k8s.io/cluster-api/util`, а ошибка поиска явно проверяется.

**`Makefile`** — версии инструментов закреплены, `setup-envtest` больше не
ставится как `@latest` (это исправляет поломку из этапа 0). golangci-lint v2
ставится из нового пути модуля (`.../golangci-lint/v2/cmd/golangci-lint`).

**`.golangci.yaml`** переписан в формате golangci-lint v2: старый формат
новая версия не читает. Набор проверок не ослаблен:

- `default: standard` — те же линтеры, что v1 включал по умолчанию;
- `revive` с правилами `exported` и `package-comments` сохранён;
- `gofmt` в v2 считается форматтером и перенесён в раздел `formatters`.

В v2 по умолчанию отключены исключения, которые v1 применял молча, поэтому
линтер нашёл старые замечания. Они исправлены в коде: непроверенная ошибка
`Close` в `scope/identity.go`, лишние обращения через встроенное поле
(`r.Client.Get` → `r.Get`).

Добавлено одно **временное** исключение: предупреждения `SA1019` об
устаревших пакетах v1beta1. На этом этапе провайдер ещё работает по старому
контракту и обязан их использовать. Исключение удалено на этапе 2.

**`Dockerfile`** — базовый образ `golang:1.26`.

**`.github/workflows/ci-base.yml`** — действия обновлены, линтер запускается
через `make lint` (версия берётся из Makefile, а не дублируется в CI),
добавлена проверка, что сгенерированные файлы совпадают с репозиторием.

**Сгенерированные файлы** (`config/crd/bases`, `zz_generated.deepcopy.go`,
`mock_client`) пересозданы новыми controller-gen и mockgen. В CRD изменились
только описания полей и ограничения длины, пришедшие из типов CAPI.

### Что сломалось по пути

1. **Последние выпуски Yandex go-sdk не компилируются.** Версии
   v0.42.0–v0.47.0 ссылаются на клиент, которого нет в go-genproto:

   ```
   go-sdk@v0.47.0/gen/dataproc/changefreeze.go:30:18: undefined: dataproc.NewChangeFreezeServiceClient
   ```

   Ошибка воспроизводится в пустом проекте, то есть это дефект самого SDK, а
   не провайдера. Перебором найдена последняя собирающаяся версия:
   **v0.41.0** (2026-10-23) с go-genproto v0.131.0. Код клиента провайдера
   (`internal/pkg/client`) с ней собирается без правок.

2. **Тесты контроллеров падали на старте** — см. выше про поиск CRD ядра.

### Что намеренно не сделано

- **Переход на go-sdk v2** (`github.com/yandex-cloud/go-sdk/v2`). Вторая
  версия SDK собирается и, судя по состоянию первой, является основной линией
  развития. Но у неё другой API, переход означает переписать весь слой
  `internal/pkg/client`, а проверить его без доступа к облаку нельзя: тесты
  провайдера работают через mock-интерфейс и этот слой не покрывают. Это
  отдельная задача, её стоит делать вместе с проверкой на живом облаке.
- **Вебхуки оставлены в пакете `api/v1alpha1`.** Правильнее вынести их в
  отдельный пакет, чтобы пакет с типами не тянул controller-runtime, но это
  перестройка сверх необходимого.

### Что осталось непроверенным

- Работа с облаком на новых go-sdk и go-genproto: проверяется только на
  живом облаке.
- Новые версии действий GitHub (`actions/checkout@v7`, `actions/setup-go@v7`):
  существование тегов проверено, сам CI запустится только после отправки
  ветки на GitHub.
