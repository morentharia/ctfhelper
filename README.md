# ctfhelper

```
go get -u github.com/morentharia/ctfhelper
```

```
ctfhelper 573315BDA197FF745F448989982093F4 https://challenge-1120.intigriti.io/qr.html\?url\=http://ya.ru\&size\=500\;width:33333\;fill:fff000\;shape-rendering:zzzzzz | head -n 40

ls *.txt | entr -rc bash -c "date; cat url.txt | xargs -I{} ctfhelper 573315BDA197FF745F448989982093F4 {} | head -n 40"
```
