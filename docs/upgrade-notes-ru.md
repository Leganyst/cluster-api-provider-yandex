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
