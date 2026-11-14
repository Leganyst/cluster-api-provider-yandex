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

## Этап 2. Контракт v1beta2

Цель этапа: чтобы ядро Cluster API воспринимало `YandexCluster` и
`YandexMachine` как объекты нового контракта и чтобы в коде провайдера не
осталось ничего, что будет удалено из Cluster API вместе с v1beta1.

Правила контракта:
[InfraCluster](https://cluster-api.sigs.k8s.io/developer/providers/contracts/infra-cluster),
[InfraMachine](https://cluster-api.sigs.k8s.io/developer/providers/contracts/infra-machine).
Образец реализации — CAPD (`test/infrastructure/docker` в репозитории CAPI).

> **Это несовместимое изменение API.** Поля меняются прямо в `v1alpha1`, без
> новой версии API и без вебхука конверсии (вариант по умолчанию из раздела 8
> требований). Версия провайдера поднимается до v0.3.0. Объекты, созданные
> провайдером v0.2, новый провайдер не «подхватит»: старые условия в
> `status.conditions` не проходят новую схему CRD. Кластеры, созданные на
> v0.2, нужно пересоздать.

### Что изменено в API (`api/v1alpha1`)

| Поле | Было | Стало | Правило контракта |
|---|---|---|---|
| `status.ready` (кластер, машина) | `bool` | удалено | заменено на `status.initialization.provisioned` |
| `status.initialization.provisioned` | — | `*bool` | «initialization completed», обязательное |
| `status.conditions` | `clusterv1.Conditions` | `[]metav1.Condition` | «conditions» |
| `status.failureReason`, `status.failureMessage` (машина) | были | удалены | «terminal failures»: в v1beta2 их больше нет |
| `spec.providerID` (машина) | `*string` | `string` | «provider ID», рекомендация контракта |
| `spec.controlPlaneEndpoint` (кластер) | тип из CAPI v1beta1 | собственный тип `APIEndpoint`, `omitempty,omitzero` | «control plane endpoint» |
| `spec.template.metadata` (шаблон машины) | `ObjectMeta` из v1beta1 | `ObjectMeta` из v1beta2, `omitzero` | руководство v1.10 → v1.11 |

Условия и причины описаны в `api/v1alpha1/types.go`:

| Объект | Условие | Причины |
|---|---|---|
| оба | `Ready` | `Ready`, `NotReady`, `ReadyUnknown` |
| оба | `Paused` | выставляет утилита CAPI `util/paused` |
| YandexCluster | `LoadBalancerReady` | `Active`, `Provisioning`, `ReconcileFailed`, `EndpointMismatch`, `Deleting`, `DeletionFailed` |
| YandexMachine | `InstanceReady` | `Running`, `Provisioning`, `NotRunning`, `NotFound`, `CreateFailed`, `ReconcileFailed`, `WaitingForBootstrapData`, `Deleting`, `DeletionFailed` |

`Ready` вычисляется перед каждой записью объекта как сводка единственного
«содержательного» условия (`LoadBalancerReady` или `InstanceReady`) —
`conditions.SetSummaryCondition`, как в CAPD. Ядро CAPI зеркалирует `Ready` в
условие `InfrastructureReady` своего `Cluster` или `Machine`.

Старые условия машины (`PROVISIONING`, `RUNNING`, `NOTFOUND`, `READY`) убраны:
их названия служили одновременно и типом, и причиной, а причины с пробелами
(`"load balancer reconcile error"`) не проходят проверку `metav1.Condition`
(причина должна быть в CamelCase).

`YandexIdentity` в контракт не входит, но его условия тоже переведены на
`metav1.Condition` — иначе в коде остался бы импорт пакетов v1beta1. Поле
`status.ready` у него сохранено.

В CRD добавлены колонки для `kubectl get` (`Provisioned`, `Ready`, `Paused`,
`State`, `Provider ID`) и категория `cluster-api`, чтобы объекты провайдера
показывались в `kubectl get cluster-api`.

### Что изменено в контроллерах

- **Чтение объектов ядра** — через `api/core/v1beta2` (сделано на этапе 1).
- **`initialization.provisioned`** ставится там же, где раньше ставился
  `status.ready = true`: `ClusterScope.SetProvisioned()` и
  `MachineScope.SetProvisioned()` (`internal/pkg/cloud/scope`).
- **Пауза.** Вместо `annotations.IsPaused` вызывается
  `paused.EnsurePausedCondition`: она выставляет условие `Paused` и сообщает,
  нужно ли остановиться. Приостановленный объект по-прежнему не
  обрабатывается: ни финализатор, ни обращения к облаку не выполняются.

  Чтобы контроллер вообще увидел приостановленный объект и смог записать
  условие, изменены фильтры событий: `ResourceNotPaused` заменён на
  `ResourceIsChanged`, а для наблюдения за `Cluster` используются
  `ClusterPausedTransitions` (контроллер кластера) и
  `ClusterPausedTransitionsOrInfrastructureProvisioned` (контроллер машин).
  Так же сделано в CAPD.
- **Остановленная ВМ.** Раньше для состояния, отличного от `RUNNING`,
  `STARTING` и `PROVISIONING`, провайдер записывал `failureReason` и
  `failureMessage`. Теперь он записывает `status.instanceState` и условие
  `InstanceReady=False` с причиной `NotRunning` (или `NotFound`), и продолжает
  опрашивать ВМ раз в 10 секунд. ВМ не перезапускается и не пересоздаётся —
  это поведение сохранено и закреплено отдельным тестом.
  `initialization.provisioned` при остановке остаётся `true`: по контракту
  это поле говорит только о первоначальном создании.
- **Финализатор.** Контроллеры кластера и identity после добавления
  финализатора возвращали `ctrl.Result{Requeue: true}`. Поле `Requeue`
  объявлено устаревшим в controller-runtime, а повтор и так происходит:
  запись финализатора меняет объект и порождает событие. Теперь возвращается
  пустой `ctrl.Result{}`.
- **Сообщения ошибок в условиях** берутся через новую функцию
  `ycerrors.Message`. Ошибки Yandex SDK содержат идентификатор запроса,
  который меняется при каждом вызове. Если писать такой текст в условие,
  объект меняется при каждом проходе, это порождает событие и немедленный
  новый проход — получается «горячий» цикл. `Message` оставляет только код и
  текст gRPC-статуса. О том же предупреждает
  [руководство v1.13 → v1.14](https://cluster-api.sigs.k8s.io/developer/providers/migrations/v1.13-to-v1.14).

### Метка контракта

`config/crd/kustomization.yaml`: метка `cluster.x-k8s.io/v1beta1: v1alpha1`
заменена на `cluster.x-k8s.io/v1beta2: v1alpha1`. По этой метке ядро CAPI
определяет, по какому контракту читать объект. Заодно устаревший
`commonLabels` заменён на `labels`.

### Расхождения с исходными требованиями

1. **`status.deprecated.v1beta1` не введён.** В требованиях сказано перенести
   туда старые условия, `failureReason` и `failureMessage`, «как в CAPD». Это
   не сделано намеренно:

   - В CAPD блок `deprecated` нужен для конверсии между его версиями API
     v1beta1 и v1beta2. У CAPY одна версия и конверсии нет, читать эти поля
     некому.
   - Ядро CAPI для провайдера с контрактом v1beta2 их не читает: оно смотрит
     на `status.initialization.provisioned` и `status.conditions`
     (`internal/contract/infrastructure_machine.go` в CAPI v1.14).
   - Чтобы хранить старые условия, нужны тип `clusterv1beta1.Conditions` и
     пакеты `util/deprecated/v1beta1`. Руководство v1.13 → v1.14 прямо
     говорит, что они будут удалены следом за API v1beta1 (CAPI v1.16). То
     есть блок `deprecated` вернул бы в код ту самую «мину» весны 2027 года,
     ради снятия которой делается обновление.

   Сейчас в коде провайдера нет ни одного импорта пакетов v1beta1.

2. **Проверка «Cluster API видит провайдер как v1beta2»** выполнена в kind на
   этапе 4, а не в envtest: в envtest нет контроллеров ядра, которые читают
   контракт.

### Тесты

Существующие проверки сохранены и переписаны на новые поля:

| Было | Стало |
|---|---|
| `status.ready == true` | `initialization.provisioned == true`, `Ready=True` и `InstanceReady`/`LoadBalancerReady=True` |
| `status.ready == false` | `provisioned` не выставлен и `Ready` не `True` |
| `failureReason != nil` при пропавшей ВМ | `InstanceReady=False`, причина `NotFound`, непустое сообщение, `Ready=False` |
| причина `NOTFOUND` при ошибке создания | `InstanceReady=False`, причина `CreateFailed` |
| `conditions == nil` при ошибке регистрации в балансировщике | `InstanceReady=False`, причина `ReconcileFailed` (раньше об этой ошибке в статусе не было ничего) |
| `result.Requeue == true` после добавления финализатора | результат пустой |
| `result.Requeue == false` | удалено там, где рядом уже проверяется `RequeueAfter`, иначе заменено на проверку `RequeueAfter == 0` |

Добавлены проверки:

- на паузе выставляется `Paused=True`, финализатор не добавляется, обращений
  к облаку нет (кластер и машина);
- у готовой машины `provisioned=true`, `providerID` в формате `yandex://<id>`,
  `Paused=False`;
- новый сценарий «ВМ остановлена и запущена снова»: ВМ создаётся один раз,
  при `STOPPED` машина становится не готовой, но `provisioned` и `providerID`
  не меняются, после `RUNNING` машина снова готова. Mock строгий: повторный
  вызов создания или удаления ВМ провалил бы тест.

Итого 35 сценариев контроллеров (было 34).

### Что осталось непроверенным

- Обновление «на месте» с v0.2 на v0.3 при существующих кластерах — не
  поддерживается и не проверялось (см. предупреждение в начале раздела).
- Поведение `MachineHealthCheck` при остановленной ВМ: условие
  `InfrastructureReady` у `Machine` станет `False`. Будет ли MHC из-за этого
  пересоздавать машину, зависит от его настройки; это пункт 5 ручной
  проверки на живом облаке. На паузе кластера MHC не работает.

## Этап 3. Шаблоны кластера

Файлы: `templates/cluster-template.yaml`,
`templates/cluster-template-with-identity.yaml`.

### Что изменено

| Что | Было | Стало |
|---|---|---|
| `Cluster`, `MachineDeployment` | `cluster.x-k8s.io/v1beta1` | `cluster.x-k8s.io/v1beta2` |
| `KubeadmControlPlane` | `controlplane.cluster.x-k8s.io/v1beta1` | `.../v1beta2` |
| `KubeadmConfigTemplate` | `bootstrap.cluster.x-k8s.io/v1beta1` | `.../v1beta2` |
| Ссылки между объектами | `apiVersion`, `kind`, `name` | `apiGroup`, `kind`, `name` |
| Ссылка на шаблон машин в KCP | `spec.machineTemplate.infrastructureRef` | `spec.machineTemplate.spec.infrastructureRef` |
| `kubeletExtraArgs` | словарь `cloud-provider: external` | список `- name: cloud-provider` / `value: external` |
| Версия Kubernetes по умолчанию | `v1.31.4` (в шаблоне с identity — `v1.25.14`) | `v1.35.10` |

Зачем: API v1beta1 ядра, KCP и CABPK объявлен устаревшим в CAPI v1.11 и
перестанет обслуживаться в v1.16. Изменения формы объектов взяты из
[руководства v1.10 → v1.11](https://release-1-11.cluster-api.sigs.k8s.io/developer/providers/migrations/v1.10-to-v1.11)
и сверены с шаблонами самого Cluster API
(`test/e2e/data/infrastructure-docker/main/bases`).

В ссылке v1beta2 нет версии API: ядро находит её по метке контракта на CRD
(`cluster.x-k8s.io/v1beta2: v1alpha1`, этап 2). Поэтому шаблон не придётся
менять, если у провайдера появится новая версия API.

`v1.35.10` — последний патч ветки 1.35 на 2026-11-14
(`https://dl.k8s.io/release/stable-1.35.txt`).

В начало шаблонов добавлен комментарий с требованиями к образу узла (раздел 6
требований): cgroup v2, containerd 2.x, драйвер cgroup `systemd`, kubeadm,
kubelet и kubectl версии 1.35.x, cloud-init, режим kube-proxy не IPVS.

### Ошибки, найденные и исправленные в шаблонах

1. **Дублирующийся ключ `name`** в `KubeadmConfigTemplate`
   (`joinConfiguration.nodeRegistration`): ключ был указан дважды. Часть
   разборщиков YAML на этом падает, остальные молча берут последнее значение.
2. **Тип диска управляющих узлов брался из переменной рабочих узлов**
   (`YANDEX_NODE_MACHINE_BOOT_DISK_TYPE`). Теперь используется
   `YANDEX_CONTROL_PLANE_MACHINE_BOOT_DISK_TYPE`. Значение по умолчанию
   прежнее (`network-ssd`). Тот, кто задавал тип диска управляющих узлов
   через переменную рабочих узлов, должен задать новую переменную.
3. **Шаблон с identity отставал от основного**: версия Kubernetes по
   умолчанию в нём была `v1.25.14`. Теперь оба шаблона отличаются только
   объектом `YandexIdentity`, полем `identityRef` и значением зоны по
   умолчанию.

### Что намеренно не сделано

- Шаблон на основе `ClusterClass` не добавлен: его не было, и в задачу он не
  входит.
- Значение зоны по умолчанию в шаблоне с identity (`ru-central1-a`) оставлено,
  хотя в основном шаблоне значения по умолчанию нет.

### Проверка

Управляющий кластер: kind v0.33.0, Kubernetes v1.37.0, Cluster API v1.14.4
(ядро, KCP, CABPK), CAPY из этой ветки, установленный через clusterctl
(этап 4).

```
clusterctl generate cluster capy-test --worker-machine-count 1 \
  --from templates/cluster-template.yaml > cluster.yaml
kubectl apply --dry-run=server -f cluster.yaml
```

Результат для обоих шаблонов: все объекты приняты сервером
(`created (server dry run)`): `YandexCluster`, `Cluster`,
`KubeadmControlPlane`, `MachineDeployment`, `KubeadmConfigTemplate`, два
`YandexMachineTemplate`, для второго шаблона ещё `YandexIdentity`.

Первая попытка проверки упала на вебхуке `YandexCluster` — это отдельная
ошибка в манифестах провайдера, она описана и исправлена на этапе 4.

### Что осталось непроверенным

- Реальное создание узлов из шаблона: нужен образ с Kubernetes 1.35 и доступ
  к облаку. Идентификатор готового образа из README (`fd8a3kknu25826s8hbq3`)
  относится к Kubernetes 1.31.4 и для версии по умолчанию не подходит.
- Команды `preKubeadmCommands` и шаблон `{{ v1.local_hostname }}` перенесены
  без изменений и на новом образе не проверялись.

## Этап 4. Выпуск для clusterctl

Цель этапа: чтобы провайдер ставился командой
`clusterctl init --infrastructure yandex`.
Правила: [clusterctl provider contract](https://cluster-api.sigs.k8s.io/developer/providers/contracts/clusterctl).

### Что изменено

**`metadata.yaml`** (новый файл в корне). Сопоставляет серии выпусков
провайдера с контрактом Cluster API: `v0.3 → v1beta2`, `v0.2` и
`v0.1 → v1beta1`. По нему clusterctl решает, совместим ли провайдер с
установленным ядром.

**Тест `api/v1alpha1/contract_test.go`** (новый). Проверяет правило
контракта: значение в `metadata.yaml` совпадает с меткой
`cluster.x-k8s.io/<контракт>` на CRD и указывает на существующую версию API.
Если поменять одно и забыть другое, тест упадёт.

**`Makefile`:**

- Путь образа задаётся переменными `REGISTRY` (по умолчанию
  `ghcr.io/leganyst`), `IMAGE_NAME`, `TAG`; итоговое значение — `IMG`, его
  по-прежнему можно задать целиком.
- Новая цель `make release` собирает в каталог `out/` (переменная
  `RELEASE_DIR`) артефакты выпуска: `infrastructure-components.yaml`,
  `metadata.yaml`, оба шаблона кластера.
- `make deploy` больше не меняет файлы под контролем git. Раньше он вызывал
  `kustomize edit set image`, что переписывало
  `config/manager/kustomization.yaml`. Теперь образ подставляется в готовый
  вывод kustomize.

**`config/default/kustomization.yaml`** — на все объекты добавлена метка
`cluster.x-k8s.io/provider: infrastructure-yandex`. Её требует контракт
clusterctl: по ней он находит объекты провайдера при обновлении, переносе и
удалении.

**`.github/workflows`:**

- `release-push.yml` — образ публикуется в GitHub Container Registry владельца
  репозитория (`ghcr.io/<владелец>/cluster-api-provider-yandex`), а не в
  реестр Yandex. Секретов не нужно, хватает токена самого workflow.
- `release-base.yml` — к выпуску GitHub прикладываются артефакты
  `make release`.

**Путь Go-модуля** оставлен `github.com/yandex-cloud/cluster-api-provider-yandex`.
Причина: смена пути затрагивает импорты во всех файлах, раздувает разницу с
оригиналом и осложняет перенос исправлений в обе стороны. На работу форка
путь модуля не влияет: модуль никто не импортирует как библиотеку.

### Что сломалось по пути (ошибки в манифестах оригинала)

Обе ошибки делают провайдер неработоспособным и не связаны с обновлением
версий: они есть в `v0.2.0`.

1. **Образ `kube-rbac-proxy` больше не существует.** Манифест добавлял к
   контроллеру второй контейнер `gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1`
   для защиты метрик. Реестр `gcr.io/kubebuilder` закрыт:

   ```
   $ docker manifest inspect gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1
   no such manifest: gcr.io/kubebuilder/kube-rbac-proxy:v0.13.1
   ```

   Под с таким контейнером не запустится (`ImagePullBackOff`).

   Исправление: второй контейнер удалён, защиту метрик выполняет сам
   controller-runtime (`filters.WithAuthenticationAndAuthorization`, тот же
   способ применяется в актуальных шаблонах kubebuilder). В `main.go` добавлен
   флаг `--metrics-secure` (по умолчанию `true`), адрес метрик по умолчанию
   стал `:8443`. Файл `config/default/manager_auth_proxy_patch.yaml` заменён
   на `manager_metrics_patch.yaml`. Роль с правами на `tokenreviews` и
   `subjectaccessreviews` уже была в `config/rbac`.

2. **Для mutating-вебхука не подставлялся сертификат.** В
   `config/default/webhookcainjection_patch.yaml` аннотация cert-manager
   была только у `ValidatingWebhookConfiguration`, а блок для
   `MutatingWebhookConfiguration` остался закомментированным с момента
   создания проекта. Mutating-вебхуки появились позже, вместе с
   `YandexIdentity`. В итоге API-сервер не доверял сертификату вебхука и
   отклонял **любое** создание или изменение `YandexCluster`:

   ```
   failed calling webhook "mutation.yandexclusters.infrastructure.cluster.x-k8s.io": ...
   tls: failed to verify certificate: x509: certificate signed by unknown authority
   ```

   Исправление: блок раскомментирован.

Кроме того:

- **`admissionReviewVersions`** у всех вебхуков изменён с `v1beta1` на `v1`.
  Версия `v1` — основная с Kubernetes 1.16, `v1beta1` сохранялась только для
  совместимости.
- **Секрет `yc-sa-key` стал необязательным** (`optional: true` в
  `bases/patches/capy-controller-manager.Deployment.yaml`). Раньше без него
  под не создавался, поэтому `clusterctl init` не мог завершиться успешно,
  пока секрет не создан, а создать его до `init` нельзя — ещё нет
  пространства имён. Теперь контроллер запускается и без ключа по умолчанию;
  кластеры без `identityRef` в этом случае получают ошибку
  `default key is not set`, кластеры с `YandexIdentity` работают.

  После создания секрета под нужно перезапустить
  (`kubectl -n capy-system rollout restart deploy/capy-controller-manager`):
  ключ читается из переменной окружения при старте.

### Проверка

Окружение: Docker Desktop 29.6.1, kind v0.33.0 (Kubernetes v1.37.0),
clusterctl v1.14.4.

1. `make release TAG=v0.3.0`, артефакты скопированы в локальный репозиторий
   `<каталог>/infrastructure-yandex/v0.3.0/`, он указан в `clusterctl.yaml`.
2. `docker build`, образ загружен в kind (`kind load docker-image`).
3. `clusterctl init --core cluster-api:v1.14.4 --bootstrap kubeadm:v1.14.4
   --control-plane kubeadm:v1.14.4`, затем
   `clusterctl init --infrastructure yandex`:

   ```
   Installing provider="infrastructure-yandex" version="v0.3.0" targetNamespace="capy-system"
   deployment "capy-controller-manager" successfully rolled out
   capy-system   infrastructure-yandex   InfrastructureProvider   yandex   v0.3.0
   ```

   Под `capy-controller-manager` в состоянии `Running`, перезапусков нет, в
   журнале при старте ошибок нет.
4. `clusterctl delete --infrastructure yandex --include-crd
   --include-namespace` и повторная установка проходят.

**Проверка контракта v1beta2 на живом ядре** (критерий готовности этапа 2).
Из шаблона создан кластер. Ключа облака в тестовом окружении нет, поэтому
готовность инфраструктуры имитировалась вручную: в `YandexCluster` записаны
`spec.controlPlaneEndpoint` и статус.

| Шаг | Что наблюдалось |
|---|---|
| Кластер создан | Провайдер выставил `Paused=False`. `Cluster` сообщает: `InfrastructureReady=False`, «YandexCluster status.initialization.provisioned is false» — ядро читает именно поле нового контракта, а не `status.ready` |
| `spec.paused: true` у `Cluster` | У `YandexCluster` появилось `Paused=True`, «Cluster spec.paused is set to true» |
| Пауза снята, в статус записано `initialization.provisioned: true` и `Ready=True` | `Cluster`: `status.initialization.infrastructureProvisioned=true`, `InfrastructureReady=True (Ready)`, `spec.controlPlaneEndpoint` скопирован из `YandexCluster` |
| Далее | KCP создал `Machine` версии v1.35.10 и `YandexMachine`; провайдер выставил на ней `Paused=False` |

Без ключа облака контроллер возвращает ошибку `default key is not set`;
повторы идут с нарастающей задержкой (одна ошибка за 30 секунд через минуту
после начала), горячего цикла нет.

Эндпоинт метрик без учётных данных отвечает отказом
(«the server has asked for the client to provide credentials»).

### Что намеренно не сделано

- **Публикация в оригинальный репозиторий** отдельными PR — по умолчанию нет
  (раздел 8 требований).
- **Сборка образа под несколько архитектур** в CI: как и раньше, собирается
  образ под архитектуру сборочной машины (amd64).
- **Уменьшение образа.** Образ занимает около 200 МБ: Yandex SDK подключает
  клиенты всех сервисов облака. Сборка с `-ldflags "-s -w"` уменьшила бы его,
  но это не относится к задаче.
- **Устаревшие поля kustomize** (`patchesStrategicMerge`, `vars`) оставлены:
  kustomize v5.7 их понимает и выдаёт предупреждение. Замена `vars` на
  `replacements` — заметная перестройка манифестов.

### Что осталось непроверенным

- Workflow выпуска (`release-push.yml`, `release-base.yml`): запускаются
  только на GitHub при создании тега. Локально проверены `make release` и
  `docker build`.
- Чтение метрик с действительным токеном (проверен только отказ без токена).
- `ServiceMonitor` из `config/prometheus` (в сборку по умолчанию не входит).
