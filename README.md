# Провайдер Yandex Cloud для Kubernetes® Cluster API

`cluster-api-provider-yandex` — провайдер для развертывания кластера Kubernetes в облачной инфраструктуре [Yandex Cloud](https://yandex.cloud) с помощью [Kubernetes Cluster API](https://cluster-api.sigs.k8s.io/).

Кластер разворачивается на базе [виртуальных машин](https://yandex.cloud/ru/docs/compute/concepts/vm) Yandex Compute Cloud и [L7-балансировщика](https://yandex.cloud/ru/docs/application-load-balancer/concepts/application-load-balancer) Yandex Application Load Balancer.

**Преимущества создания кластера с помощью провайдера Yandex Cloud**

* интеграция с API Yandex Cloud;
* декларативный подход к созданию и управлению кластером;
* кластер как [CustomResourceDefinition](https://kubernetes.io/docs/tasks/extend-kubernetes/custom-resources/custom-resource-definitions/);
* широкий спектр параметров для конфигурации вычислительных ресурсов кластера;
* [пользовательские образы](#подготовьте-образ-ос-для-узлов-кластера) операционных систем для мастера и узлов;
* пользовательский Control Plane;
* альтернатива [Terraform](https://www.terraform.io/) в CI-процессах.

## Совместимость провайдера с Cluster API

| Версия провайдера | Версия API провайдера | Cluster API | Контракт Cluster API | Kubernetes в управляющем кластере | Kubernetes в создаваемом кластере |
| :---: | :---: | :---: | :---: | :---: | :---: |
| v0.3.x | v1alpha1 | v1.14 | v1beta2 | 1.33–1.37 | 1.31–1.37 (по умолчанию 1.35) |
| v0.1.x, v0.2.x | v1alpha1 | v1.6 | v1beta1 | — | 1.31 |

Диапазоны версий Kubernetes для v0.3 взяты из [таблицы версий Cluster API](https://cluster-api.sigs.k8s.io/reference/versions) для выпуска v1.14.

> [!IMPORTANT]
> Версия v0.3 несовместима с v0.2: поля `status` ресурсов `YandexCluster` и `YandexMachine` изменены под контракт v1beta2 без новой версии API. Кластеры, созданные провайдером v0.2, нужно пересоздать. Что и почему изменено, описано в [заметках об обновлении](docs/upgrade-notes-ru.md).

Версию Kubernetes в создаваемом кластере определяет не провайдер, а `kubeadm` и `kubelet` из [образа ОС узлов](#подготовьте-образ-ос-для-узлов-кластера). Провайдер отвечает за виртуальные машины, сеть и балансировщик.

Чтобы развернуть кластер Kubernetes в Yandex Cloud с помощью Cluster API:
1. [Подготовьте облако к работе](#подготовьте-облако-к-работе).
1. [Настройте окружение](#настройте-окружение).
1. [Подготовьте образ ОС для узлов кластера](#подготовьте-образ-ос-для-узлов-кластера).
1. [Получите Docker-образ с провайдером Yandex Cloud](#получите-docker-образ-с-провайдером-yandex-cloud).
1. [Установите провайдер Yandex Cloud и провайдер Kubernetes Cluster API](#установите-провайдер-yandex-cloud-и-провайдер-kubernetes-cluster-api).
1. [Сформируйте манифесты кластера](#сформируйте-манифесты-кластера).
1. [Разверните кластер](#разверните-кластер).
1. [Подключитесь к кластеру](#подключитесь-к-кластеру).
1. [Установите в созданный кластер CCM](#установите-в-созданный-кластер-ccm).
1. [Установите в созданный кластер CNI](#установите-в-созданный-кластер-cni).
1. [Проверьте связь управляющего кластера с созданным](#проверьте-связь-управляющего-кластера-с-созданным).

Если созданные ресурсы вам больше не нужны, [удалите](#как-удалить-созданные-ресурсы) их.

## Подготовьте облако к работе

Зарегистрируйтесь в Yandex Cloud и создайте [платежный аккаунт](https://yandex.cloud/ru/docs/billing/concepts/billing-account):

1. Перейдите в [консоль управления](https://console.yandex.cloud/), затем войдите в Yandex Cloud или зарегистрируйтесь.
1. На странице **[Yandex Cloud Billing](https://billing.yandex.cloud/accounts)** убедитесь, что у вас подключен платежный аккаунт, и он находится в [статусе](https://yandex.cloud/ru/docs/billing/concepts/billing-account-statuses) `ACTIVE` или `TRIAL_ACTIVE`. Если платежного аккаунта нет, [создайте](https://yandex.cloud/ru/docs/billing/quickstart/) его и [привяжите](https://yandex.cloud/ru/docs/billing/operations/pin-cloud) к нему облако.

    Если у вас есть активный платежный аккаунт, вы можете создать или выбрать [каталог](https://yandex.cloud/ru/docs/resource-manager/concepts/resources-hierarchy#folder), в котором будет работать ваша инфраструктура, на [странице облака](https://console.yandex.cloud/cloud).

    [Подробнее об облаках и каталогах](https://yandex.cloud/ru/docs/resource-manager/concepts/resources-hierarchy)

### Платные ресурсы

В стоимость поддержки инфраструктуры кластера входят:
* плата за вычислительные ресурсы ВМ, диски и образы (см. [тарифы Yandex Compute Cloud](https://yandex.cloud/ru/docs/compute/pricing));
* плата за хранение образа в бакете и операции с данными (см. [тарифы Yandex Object Storage](https://yandex.cloud/ru/docs/storage/pricing));
* плата за использование вычислительных ресурсов L7-балансировщика (см. [тарифы Yandex Application Load Balancer](https://yandex.cloud/ru/docs/application-load-balancer/pricing));
* (опционально) плата за использование динамического внешнего IP-адреса для вспомогательной ВМ (см. [тарифы Yandex Virtual Private Cloud](https://yandex.cloud/ru/docs/vpc/pricing#prices-public-ip));
* (опционально) плата за использование управляющего кластера (см. [тарифы Yandex Managed Service for Kubernetes](https://yandex.cloud/ru/docs/managed-kubernetes/pricing)).

### Подготовьте инфраструктуру

1. Настройте [сервисный аккаунт](https://yandex.cloud/ru/docs/iam/concepts/users/service-accounts) Yandex Cloud:
    1. [Создайте](https://yandex.cloud/ru/docs/iam/operations/sa/create) сервисный аккаунт, от имени которого будут создаваться ресурсы кластеры.
    1. [Назначьте](https://yandex.cloud/ru/docs/iam/operations/sa/assign-role-for-sa) сервисному аккаунту роли [compute.editor](https://yandex.cloud/ru/docs/compute/security/#compute-editor) и [alb.editor](https://yandex.cloud/ru/docs/application-load-balancer/security/#alb-editor) на каталог.
    1. [Получите](https://yandex.cloud/ru/docs/iam/operations/authorized-key/create) авторизованный ключ для сервисного аккаунта в формате JSON.
1. Если в вашем каталоге еще нет [облачной сети](https://yandex.cloud/ru/docs/vpc/concepts/network#network) Virtual Private Cloud, [создайте](https://yandex.cloud/ru/docs/vpc/operations/network-create) ее. Также создайте [подсеть](https://yandex.cloud/ru/docs/vpc/operations/subnet-create).
1. Инфраструктуре создаваемого кластера в Virtual Private Cloud назначается [группа безопасности](https://yandex.cloud/ru/docs/vpc/concepts/security-groups) по умолчанию. [Добавьте](https://yandex.cloud/ru/docs/vpc/operations/security-group-add-rule) в эту группу следующие правила для _входящего_ трафика:

    | Протокол | Диапазон портов | Тип источника | Источник | Описание |
    | --- | --- | --- | --- | --- |
    | `TCP` | `0-65535` | `Группа безопасности` | `Balancer` | Проверки состояния L7-балансировщиком |
    | `Any` | `8443` | `CIDR` | `0.0.0.0/0` | Доступ к Kubernetes API |

1. Создаваемый кластер будет доступен в облачной сети по [внутреннему IP-адресу](https://yandex.cloud/ru/docs/vpc/concepts/address#internal-addresses). Чтобы обеспечить удаленный доступ в кластер, [создайте](https://yandex.cloud/ru/docs/compute/operations/vm-create/create-linux-vm) вспомогательную ВМ в той же сети, в которой будет развернут кластер, и с той же группой безопасности. Установите на ВМ [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl).

1. Создайте _управляющий_ [кластер](https://yandex.cloud/ru/docs/managed-kubernetes/operations/kubernetes-cluster/kubernetes-cluster-create) и [группу узлов](https://yandex.cloud/ru/docs/managed-kubernetes/operations/node-group/node-group-create) Yandex Managed Service for Kubernetes. Из этого кластера будут осуществляться развертывание нового кластера с помощью Cluster API и управление кластерной инфраструктурой.

    Также вы можете развернуть управляющий кластер локально, например с помощью утилиты [kind](https://kind.sigs.k8s.io/).

> [!IMPORTANT]
> Чтобы иметь возможность загружать Docker-образы c компонентами, которые будут развернуты внутри управляющего и создаваемого кластеров, у этих кластеров должен быть доступ в интернет. Например, вы можете [настроить NAT-шлюз](https://yandex.cloud/ru/docs/vpc/operations/create-nat-gateway) в подсетях кластеров.

## Настройте окружение

1. Установите следующие инструменты:
    * [Go](https://go.dev/doc/install) версии 1.26.0 и выше (нужен только для сборки из исходного кода);
    * [docker](https://www.docker.com/) версии 17.03 и выше;
    * [kubectl](https://kubernetes.io/docs/tasks/tools/#kubectl) версии, совместимой с управляющим кластером;
    * [clusterctl](https://cluster-api.sigs.k8s.io/user/quick-start#install-clusterctl) версии 1.14.x.

1. Настройте для `kubectl` доступ к управляющему кластеру Kubernetes версии 1.33–1.37:
    * [Managed Service for Kubernetes](https://yandex.cloud/ru/docs/managed-kubernetes/operations/connect/#kubectl-connect);
    * [kind](https://kind.sigs.k8s.io/docs/user/quick-start/#interacting-with-your-cluster).

1. Склонируйте репозиторий `cluster-api-provider-yandex` и перейдите в директорию с проектом:

    ```bash
    git clone https://github.com/k8s-pier/cluster-api-provider-yandex.git
    cd cluster-api-provider-yandex
    ```

## Подготовьте образ ОС для узлов кластера

[Образ](https://yandex.cloud/ru/docs/compute/concepts/image) ОС, который будет развернут на узлах создаваемого кластера, должен быть подготовлен для работы с Kubernetes Cluster API, а также совместим с Compute Cloud.

Версия Kubernetes в образе должна совпадать с версией в манифесте кластера (переменная `KUBERNETES_VERSION`, по умолчанию `v1.35.10`).

### Требования к образу для Kubernetes 1.35

| Требование | Почему |
| --- | --- |
| `kubeadm`, `kubelet`, `kubectl` версии 1.35.x и `cloud-init` | Узел настраивается через `cloud-init`, который запускает `kubeadm` из образа |
| cgroup v2 | В Kubernetes 1.35 `kubelet` по умолчанию не запускается на узле с cgroup v1 (`failCgroupV1: true`) |
| containerd 2.x | Kubernetes 1.35 — последняя версия с поддержкой containerd 1.x; с containerd 1.x узел нельзя будет обновить до 1.36 |
| Драйвер cgroup `systemd` и у containerd, и у `kubelet` | При разных драйверах `kubelet` не запускает поды |
| Режим kube-proxy `iptables` или `nftables` | Режим IPVS в Kubernetes 1.35 объявлен устаревшим |

> [!IMPORTANT]
> Готовый образ `fd8a3kknu25826s8hbq3` (Ubuntu 24.04) из оригинального репозитория подготовлен для Kubernetes **1.31.4**. С версией по умолчанию он не подойдет. Чтобы использовать его, задайте `KUBERNETES_VERSION=v1.31.4` при [формировании манифеста кластера](#сформируйте-манифесты-кластера). Образ создан в ознакомительных целях, использовать его в промышленной эксплуатации не рекомендуется.

Чтобы подготовить собственный образ ОС:

1. [Соберите](https://image-builder.sigs.k8s.io/capi/capi) образ ОС с помощью утилиты [Image Builder](https://github.com/kubernetes-sigs/image-builder), указав версию Kubernetes 1.35.x.

    См. также [Подготовить образ диска для Compute Cloud](https://yandex.cloud/ru/docs/compute/operations/image-create/custom-image).

1. [Загрузите](https://yandex.cloud/ru/docs/compute/operations/image-create/upload) образ в Compute Cloud и сохраните его идентификатор.

## Получите Docker-образ с провайдером Yandex Cloud

Вы можете [использовать готовый Docker-образ](#использовать-готовый-docker-образ) или [собрать его самостоятельно](#собрать-docker-образ-из-исходного-кода) из исходного кода.

### Использовать готовый Docker-образ

При создании тега выпуска образ этого форка публикуется в GitHub Container Registry (workflow `.github/workflows/release-push.yml`):

```text
ghcr.io/k8s-pier/cluster-api-provider-yandex:<тег_выпуска>
```

Путь к образу уже записан в файле `infrastructure-components.yaml` соответствующего выпуска, отдельно указывать его не нужно.

### Собрать Docker-образ из исходного кода

1. Выберите реестр, доступный управляющему кластеру, и аутентифицируйтесь в нем (`docker login`).

1. Запустите Docker daemon.

1. Соберите Docker-образ и загрузите его в реестр. Путь к образу складывается из переменных `REGISTRY`, `IMAGE_NAME` и `TAG`:

    ```bash
    make docker-build docker-push REGISTRY=<реестр> TAG=<тег>
    ```

    Например, `REGISTRY=cr.yandex/<идентификатор_реестра> TAG=v0.3.0` даст образ `cr.yandex/<идентификатор_реестра>/cluster-api-provider-yandex:v0.3.0`. Полный путь можно задать и одной переменной `IMG`.

1. Соберите артефакты выпуска для `clusterctl` с тем же образом:

    ```bash
    make release REGISTRY=<реестр> TAG=<тег>
    ```

    В каталоге `out/` появятся файлы `infrastructure-components.yaml`, `metadata.yaml` и шаблоны кластера.

## Установите провайдер Yandex Cloud и провайдер Kubernetes Cluster API

Провайдер устанавливается утилитой `clusterctl`. Провайдера Yandex Cloud нет во встроенном списке `clusterctl`, поэтому его репозиторий нужно указать в файле конфигурации.

1. Добавьте провайдер в конфигурацию `clusterctl` (по умолчанию `$XDG_CONFIG_HOME/cluster-api/clusterctl.yaml`):

    * чтобы установить опубликованный выпуск:

        ```yaml
        providers:
          - name: "yandex"
            url: "https://github.com/k8s-pier/cluster-api-provider-yandex/releases/latest/infrastructure-components.yaml"
            type: "InfrastructureProvider"
        ```

    * чтобы установить артефакты, собранные командой `make release`, скопируйте каталог `out/` в `<каталог>/infrastructure-yandex/<тег>/` (например, `~/capy-repo/infrastructure-yandex/v0.3.0/`) и укажите путь к файлу:

        ```yaml
        providers:
          - name: "yandex"
            url: "file:///<каталог>/infrastructure-yandex/<тег>/infrastructure-components.yaml"
            type: "InfrastructureProvider"
        ```

1. Установите в управляющий кластер Cluster API, [cert-manager](https://cert-manager.io/) и провайдер Yandex Cloud:

    ```bash
    clusterctl init \
      --core cluster-api:v1.14.4 \
      --bootstrap kubeadm:v1.14.4 \
      --control-plane kubeadm:v1.14.4 \
      --infrastructure yandex
    ```

1. Создайте секрет с авторизованным ключом сервисного аккаунта Yandex Cloud и перезапустите провайдер, чтобы он прочитал ключ:

    ```bash
    kubectl create secret generic yc-sa-key \
      --from-file=key=<путь_к_файлу_с_авторизованным_ключом> \
      --namespace capy-system

    kubectl rollout restart deployment capy-controller-manager \
      --namespace capy-system
    ```

    Этот ключ используется для кластеров, у которых не задан `identityRef`. Если каждый кластер ссылается на собственный ресурс `YandexIdentity` (шаблон `templates/cluster-template-with-identity.yaml`), секрет `yc-sa-key` не нужен.

1. Убедитесь, что провайдер запущен:

    ```bash
    kubectl get providers --all-namespaces
    kubectl get pods --namespace capy-system
    ```

<details>
<summary>Установка без clusterctl (для разработки)</summary>

После `clusterctl init` без `--infrastructure yandex` выполните:

```bash
make deploy IMG=<путь_к_образу>
```

Команда установит CRD и контроллер из каталога `bases/` в пространство имен `capy-system`.

</details>

## Сформируйте манифесты кластера

1. Выберите [зону доступности](https://yandex.cloud/ru/docs/overview/concepts/geo-scope), в которой вы хотите развернуть кластер.

1. Получите идентификаторы ресурсов Yandex Cloud для развертывания кластера:
    * [образ ОС](https://yandex.cloud/ru/docs/compute/operations/image-control/image-control-get-info);
    * [каталог](https://yandex.cloud/ru/docs/resource-manager/operations/folder/get-id);
    * [сеть](https://yandex.cloud/ru/docs/vpc/operations/network-get-info);
    * [подсеть](https://yandex.cloud/ru/docs/vpc/operations/subnet-get-info) в выбранной зоне доступности.

1. Передайте идентификаторы ресурсов в переменные окружения:

    ```bash
    export YANDEX_CONTROL_PLANE_MACHINE_IMAGE_ID=<идентификатор_образа>
    export YANDEX_FOLDER_ID=<идентификатор_каталога>
    export YANDEX_NETWORK_ID=<идентификатор_сети>
    export YANDEX_SUBNET_ID=<идентификатор_подсети>
    export YANDEX_ZONE_ID=<идентификатор_зоны_доступности>
    ```

1. Сформируйте манифесты кластера:

    ```bash
    clusterctl generate cluster <имя_создаваемого_кластера> \
      --from templates/cluster-template.yaml > /tmp/capy-cluster.yaml
    ```

    По умолчанию согласно сгенерированному манифесту, для доступа в кластер будет развернут [L7-балансировщик](https://yandex.cloud/ru/docs/application-load-balancer/concepts/application-load-balancer) Application Load Balancer c динамическим внутренним IP-адресом. Вы можете [присвоить L7-балансировщику фиксированный IP-адрес](#Опционально-настройте-эндпоинт-api-сервера).

> [!IMPORTANT]
> После создания кластера, присвоить L7-балансировщику фиксированный IP-адрес будет нельзя.

Согласно сгенерированному манифесту будут развернуты три узла с Control Plane. Если вы хотите также сразу развернуть узлы для рабочей нагрузки, выполните команду:

```bash
clusterctl generate cluster <имя_создаваемого_кластера> \
  --worker-machine-count <количество_узлов_для_рабочей_нагрузки> \
  --from templates/cluster-template.yaml > /tmp/capy-cluster.yaml
```

### (Опционально) Настройте эндпоинт API-сервера

Задайте в манифесте `YandexCluster` следующие параметры для L7-балансировщика:

```yaml
  loadBalancer:
    listener:
      address: <фиксированный_IP-адрес_из_диапазона_подсети>
      subnet:
        id: <идентификатор_подсети>
```

## Разверните кластер

```bash
kubectl apply -f /tmp/capy-cluster.yaml
```

За процессом развертывания кластера можно следить в [консоли управления](https://console.yandex.cloud/) Yandex Cloud, а также в логах пода `capy-controller-manager`:

```bash
kubectl logs <имя_пода_с_capy-controller-manager> \
  --namespace capy-system \
  --follow
```

## Подключитесь к кластеру

Реквизиты для подключения к новому кластеру будут созданы в управляющем кластере в секрете `<имя_создаваемого_кластера>-kubeconfig`.

1. Получите данные из секрета:

    ```bash
    kubectl get secret <имя_создаваемого_кластера>-kubeconfig \
      --output yaml | yq -r '.data.value' | base64 \
      --decode > capy-cluster-config
    ```

1. Передайте на ВМ, находящейся в той же сети, в которой расположен новый кластер, файл с конфигурацией для `kubectl`:

    ```bash
    scp <путь_к_файлу_capy-cluster-config_на_локальном_компьютере> \
    <имя_пользователя>@<публичный_IP-адрес_ВМ>:/home/<имя_пользователя>/.kube/config
    ```

1. [Подключитесь](https://yandex.cloud/ru/docs/compute/operations/vm-connect/ssh) к ВМ по SSH.

1. Подключитесь к новому кластеру:

    ```bash
    kubectl cluster-info
    ```

## Установите в созданный кластер CCM

Для обеспечения связи между ресурсами кластера и ресурсами Yandex Cloud, установите в созданный кластер [Cloud Controller Manager](https://kubernetes.io/docs/concepts/architecture/cloud-controller/), например [Kubernetes Cloud Controller Manager for Yandex Cloud](https://github.com/deckhouse/yandex-cloud-controller-manager/tree/master).

> [!IMPORTANT]
> Если вы хотите использовать решение Kubernetes Cloud Controller Manager for Yandex Cloud, добавьте в пример манифеста [yandex-cloud-controller-manager.yaml](https://github.com/deckhouse/yandex-cloud-controller-manager/blob/master/manifests/yandex-cloud-controller-manager.yaml) для `DaemonSet` актуальную версию Docker-образа и переменную окружения `YANDEX_CLUSTER_NAME` с именем созданного кластера в значении.

## Установите в созданный кластер CNI

Чтобы обеспечить сетевую функциональность для подов в новом кластере, установите в него [Container Network Interface](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/network-plugins/), например [Cilium](https://github.com/cilium/cilium) или [Calico](https://github.com/projectcalico/calico).

Подробнее см. в документации:
* [Cilium Quick Installation](https://docs.cilium.io/en/stable/gettingstarted/k8s-install-default/)
* [Quickstart for Calico on Kubernetes](https://docs.tigera.io/calico/latest/getting-started/kubernetes/quickstart)

## Проверьте связь управляющего кластера с созданным

1. На ВМ, находящейся в той же сети, в которой расположен новый кластер, убедитесь, что все поды с необходимыми системными компонентами развернуты в кластере:

    ```bash
    kubectl get pods --all-namespaces
    ```

    Пример вывода:

    ```bash
    NAMESPACE     NAME                                                       READY   STATUS    RESTARTS   AGE
    kube-system   calico-kube-controllers-695bcfd99c-rcc42                   1/1     Running   0          3h55m
    kube-system   calico-node-9qhxj                                          1/1     Running   0          3h55m
    kube-system   coredns-7c65d6cfc9-52tvn                                   1/1     Running   0          4h50m
    kube-system   coredns-7c65d6cfc9-dpgvg                                   1/1     Running   0          4h50m
    kube-system   etcd-capy-cluster-control-plane-p646q                      1/1     Running   0          4h50m
    kube-system   kube-apiserver-capy-cluster-control-plane-p646q            1/1     Running   0          4h50m
    kube-system   kube-controller-manager-capy-cluster-control-plane-p646q   1/1     Running   0          4h50m
    kube-system   kube-proxy-wb7jr                                           1/1     Running   0          4h50m
    kube-system   kube-scheduler-capy-cluster-control-plane-p646q            1/1     Running   0          4h50m
    kube-system   yandex-cloud-controller-manager-nwhwv                      1/1     Running   0          26s
    ```

1. На локальном компьютере проверьте связь управляющего кластера с созданным:

    ```bash
    clusterctl describe cluster <имя_созданного_кластера>
    ```

    Пример вывода:

    ```bash
    NAME                                                             READY  SEVERITY  REASON  SINCE  MESSAGE                                  
    Cluster/capy-cluster                                             True                     10s
    ├─ClusterInfrastructure - YandexCluster/capy-cluster                                   
    └─ControlPlane - KubeadmControlPlane/capy-cluster-control-plane  True                     10s                                                                                       
      └─3 Machines...                                                True                     3m9s   See capy-cluster-control-plane-cf72l, capy-cluster-control-plane-g9jw7, ...
    ```

## Как удалить созданные ресурсы

### Удалите кластер

```bash
kubectl delete -f /tmp/capy-cluster.yaml
```

### Удалите CRD из управляющего кластера

```bash
make uninstall
```

### Удалите контроллер провайдера Yandex Cloud из управляющего кластера

```sh
make undeploy
```

### Удалите вспомогательные ресурсы Yandex Cloud

Чтобы перестать платить за вспомогательные ресурсы, если вы их создавали, удалите:
* [Группу узлов Managed Service for Kubernetes](https://yandex.cloud/ru/docs/managed-kubernetes/operations/node-group/node-group-delete)
* [Кластер Managed Service for Kubernetes](https://yandex.cloud/ru/docs/managed-kubernetes/operations/kubernetes-cluster/kubernetes-cluster-delete)
* [ВМ Compute Cloud](https://yandex.cloud/ru/docs/compute/operations/vm-control/vm-delete)
* [Образ ОС в Compute Cloud](https://yandex.cloud/ru/docs/compute/operations/image-control/delete)
* [Образ ОС в Object Storage](https://yandex.cloud/ru/docs/storage/operations/objects/delete)
* [Бакет Object Storage](https://yandex.cloud/ru/docs/storage/operations/buckets/delete)

## См. также

* [Apache-2.0 license](LICENSE)
* [Соглашение с контрибьютором](CONTRIBUTING.md)
* [Kubernetes Cluster API](https://cluster-api.sigs.k8s.io/)
* [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)
