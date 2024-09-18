# Mocker

Application to mock HTTP endpoints via a yaml file

## Configuration

```yaml
port: 8081        #sets listening port
routes:
  "/ready":
    get:
      - response: OK    #no coditions always returns OK
  "/login":
    post:
      - conditions:
        - header_eq "Authorization" "Basic dXNlcjpwYXNzd29yZA=="  #checks if header Authorization is equal to Basic dXNlcjpwYXNzd29yZA==
        response: '{"token": "9cc2398d499b689de63c73a910803fa07e192c8c"}'
        headers:
          "content-type": application/json #can set response headers
      
      - response: Unauthorized    #if no previous conditions are met, returns this reponse
        code: 403
```

Save this as mocker.yaml

## Usage

```bash
mocker #in the directory with mocker.yaml
```

```bash
curl -i http://localhost:8081/ready 
HTTP/1.1 200 OK
Date: Wed, 18 Sep 2024 22:58:39 GMT
Content-Length: 2
Content-Type: text/plain; charset=utf-8

OK
```

```bash
curl -i -X POST http://localhost:8081/login
HTTP/1.1 403 Forbidden
Date: Wed, 18 Sep 2024 22:59:45 GMT
Content-Length: 12
Content-Type: text/plain; charset=utf-8

Unauthorized
```


```bash
curl -i -X POST -u "user:password" http://localhost:8081/login
HTTP/1.1 200 OK
Content-Type: application/json
Date: Wed, 18 Sep 2024 23:17:55 GMT
Content-Length: 53

{"token": "9cc2398d499b689de63c73a910803fa07e192c8c"}
```