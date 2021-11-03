#!/bin/bash

DATE=`date +%Y%m%d`

docker build -f Dockerfile.out -t "out:${DATE}" .

docker build -f Dockerfile.in -t "in:${DATE}" .
