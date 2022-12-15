# sealed-secrets-addon

[sealed-secrets-controller](https://github.com/bitnami-labs/sealed-secrets) 를 보완하기 위한 사이드카 역할을 하는 애드온입니다.

애드온의 주요 역할은 sealed-secrets-controller 에 의해 복호화된 secret 과 동일한 데이터의 secret 복사본을 만드는 것입니다.</br>
이는 기존의 "sealed-secret - secret" 의 연결된 관계에서 벗어난 독립적인 secret 을 어플리케이션에 제공하기 위함입니다.</br>
만약 sealed-secrets-controller 에서 장애가 발생한 경우에도 어플리케이션은 복사된 secret 을 사용하기 때문에 컨트롤러의 장애에 아무런 영향을 받지 않고 정상적으로 secret 데이터에 접근이 가능합니다.

아래는 sealed-secrets-controller 을 사용하는 배포 과정을 나타내는 흐름입니다

<img src="https://user-images.githubusercontent.com/61482763/207762375-6b22a286-6722-4f58-bbb0-8dd45b5cc536.png" width="90%" height="90%">
</br></br>

위 흐름에서 sealed-secrets-controller 의 사이드카로 있는 sealed-secrets-addon 은 아래와 같은 흐름으로 secret 을 복사합니다

<img src="https://user-images.githubusercontent.com/61482763/205541965-b75d9cf1-ddda-4513-b417-f5966e08c718.png" width="90%" height="90%">
</br></br>

해당 애드온을 통해 좀 더 안정적으로 secret 데이터를 어플리케이션에게 제공할 수 있습니다.
