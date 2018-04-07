# gobm65

gobm65 is a Beurer BM65 Blood Pressure Monitor CLI reader

## Installation:

Use go get to build the utility, either from Mercurial
or from the Github mirror:

```
% go get hg.lilotux.net/golang/mikael/gobm65
```
or
```
% go get github.com/McKael/gobm65
```

## Examples:

Get help:
```
% gobm65 --help
```

Get records and display the average:
```
% gobm65 --average
```
... display more statistics:
```
% gobm65 --stats
```
... add WHO classification:
```
% gobm65 --stats --class
```

Display the latest 3 records with the average:
```
% gobm65 -l 3 --average
```

Display all records since a specific date:
```
% gobm65 --since "2016-06-01"
```

Display all records before a specific date:
```
% gobm65 --to-date "2016-06-30"
```

Display all records of the last 7 days:
```
% gobm65 --since "$(date "+%F" -d "7 days ago")"
```

Display statistics for morning records:
```
% gobm65 --from-time 06:00 --to-time 12:00 --stats
```

One can invert times to get night data:
```
% gobm65 --from-time 21:00 --to-time 09:00
```

Display the last/first 10 records in JSON:
```
% gobm65 -l 10 --format json
```

Save the records to a JSON file:
```
% gobm65 -o data_u2.json
```

Read a JSON file and display average of the last 3 records:
```
% gobm65 -i data_u2.json -l 3 --average
% gobm65 -i data_u2.json -l 3 --stats
```

Read a JSON file, merge with device records, and save to another file:
```
% gobm65 -i data_u2.json --merge -o data_u2-new.json
```

Data from several JSON files can be merged, files are separated with a ';':
```
% gobm65 -i "data_u0.json;data_u1.json;data_u2.json"
```

## Credits

Thanks to atbrask for figuring out the protocol details and writing a
nice [blog post](<http://www.atbrask.dk/?p=98>).
