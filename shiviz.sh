#!/bin/bash
FILE=Shiviz.log
# echo '(?<host>\S*) (?<clock>{.*})\n(?<event>.*)' > $FILE
echo -e "\n\n" >> $FILE
cat *g.txt >> $FILE

