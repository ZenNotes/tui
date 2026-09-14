// From the repository root: go run ./internal/periodic/cmd/localedata | node internal/periodic/cmd/localedata/generate.mjs > internal/periodic/date_names.json
// Intl/ICU supplies the same standalone month and weekday names as the desktop.
let input='';for await (const chunk of process.stdin) input+=chunk;
const out={};
for(const locale of Intl.DateTimeFormat.supportedLocalesOf(JSON.parse(input))){
 const names={};
 for(const [token,field,length,count] of [['MMMM','month','long',12],['MMM','month','short',12],['EEEE','weekday','long',7],['EEE','weekday','short',7]]){
  const fmt=new Intl.DateTimeFormat(locale,{[field]:length,timeZone:'UTC'});
  names[token]=Array.from({length:count},(_,i)=>fmt.format(new Date(Date.UTC(2026,field==='month'?i:2,field==='month'?1:1+i))));
 }
 out[locale]=names;
}
process.stdout.write(JSON.stringify(out));
