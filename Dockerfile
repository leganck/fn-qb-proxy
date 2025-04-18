FROM scratch
LABEL authors="leganck"
COPY  fn-qb-proxy /
ENV  UDS="/app/qbt.sock" \
     PWD-FILE="/app/qb-pwd" \
     PORT=8086 \
     PASSWORD="admin"

CMD ["/fn-qb-proxy"]
