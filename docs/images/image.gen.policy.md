
## curl 예시

```
curl --request POST \
  --url https://api.siliconflow.com/v1/images/generations \
  --header 'Authorization: Bearer <token>' \
  --header 'Content-Type: application/json' \
  --data '
{
  "model": "Qwen/Qwen-Image",
  "prompt": "here is promopts",
  "image_size": "1664x928"
}
'
```

## 캐릭터 생성

model: Qwen/Qwen-Image
image_size: 1664x928


## 씬 이미지 생성

### 캐릭터가 포함된 경우.

model: Qwen/Qwen-Image-Edit
image_size: 1664x928
image: 캐릭터에서 생성한 이미지 (ex. "data:image/png;base64, XXX")

### 캐릭터가 포함되지 않은 경우.

model: Qwen/Qwen-Image
image_size: image_size: 1664x928