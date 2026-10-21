package dev.local.aiphone;

import android.app.Activity;
import android.app.AlertDialog;
import android.graphics.Color;
import android.graphics.Typeface;
import android.graphics.drawable.GradientDrawable;
import android.text.InputFilter;
import android.view.Gravity;
import android.widget.*;
import org.json.JSONArray;
import org.json.JSONObject;
import java.text.SimpleDateFormat;
import java.util.Date;
import java.util.Locale;
import java.util.UUID;

/** Self-mode overview. Personal tasks stay on this device; remote cards are bounded saved notes. */
final class SelfHomeView extends ScrollView {
    private static final int TEXT=0xffebefe8, MUTED=0xff9baa9d, GOLD=0xffeeb96e, CARD=0xff1b2e25;
    private final Activity activity;
    private final Runnable talk, refresh;
    private final java.util.function.Consumer<String> navigate;
    private final LinearLayout body;
    private JSONArray tasks = new JSONArray(), notes = new JSONArray();
    private JSONObject brief = new JSONObject();
    private String analysis = "", analysisStatus = "";
    private boolean weekly, loading;
    Runnable analyze;
    java.util.function.Consumer<String> draftQuestion;
    private android.view.View setupView;
    private int generation;
    boolean hasEvidence(){return notes.length()>0;}
    int generation(){return generation;}
    void setSetupView(android.view.View view){setupView=view;render();}
    void clearBrief(){generation++;brief=new JSONObject();notes=new JSONArray();analysis="";analysisStatus="";loading=false;briefStatus="Connect Cully to bring in your last 24 hours and past week.";render();}
    String briefSnapshot(){return brief.toString();}
    void analysisStarted(){analysisStatus="Finding a useful next step on your phone…";render();}
    void analysisFinished(String text){analysis=text;analysisStatus="Local AI draft · check the dated evidence below";render();}
    void analysisFailed(){analysisStatus="Local analysis unavailable. Your dated notes are still below.";render();}
    private String page = "Today", focusId, briefStatus = "Connect Cully, then refresh to bring in recent work.";

    SelfHomeView(Activity activity, Runnable talk, Runnable refresh, java.util.function.Consumer<String> navigate) {
        super(activity); this.activity=activity; this.talk=talk; this.refresh=refresh; this.navigate=navigate;
        body=column(); body.setPadding(0, dp(10), 0, dp(18)); addView(body);
        try { tasks = new JSONArray(activity.getPreferences(0).getString("self_tasks", "[]")); } catch (Exception ignored) {}
        focusId=activity.getPreferences(0).getString("self_focus", "");
        render();
    }
    void showPage(String name) { page=name; render(); smoothScrollTo(0,0); }
    void loadingBrief() { generation++; loading=true; briefStatus="Gathering your last 24 hours and past week…"; render(); }
    void briefFailed(String message) { loading=false; briefStatus=message; render(); }
    void updateBrief(String raw) {
        try {
            brief=new JSONObject(raw); notes=new JSONArray();
            for(String key:new String[]{"last24h","earlier6days"}){JSONArray rows=brief.getJSONObject(key).getJSONArray("notes");for(int i=0;i<rows.length();i++)notes.put(rows.get(i));}
            analysis="";analysisStatus="";loading=false;
            briefStatus="Refreshed " + new SimpleDateFormat("h:mm a",Locale.getDefault()).format(new Date()) + " · saved notes, not live activity";
        } catch (Exception invalid) { loading=false; briefStatus="Could not read the brief. Your local tasks are still available."; }
        render();
    }
    private void render() {
        body.removeAllViews();
        if (page.equals("Tasks")) tasksPage(); else if (page.equals("Advisor")) advisorPage(); else todayPage();
    }
    private void todayPage() {
        text(body,new SimpleDateFormat("EEEE, MMM d",Locale.getDefault()).format(new Date()).toUpperCase(Locale.getDefault()),11,GOLD);
        title("Today starts here.");
        text(body,"Pick up the thread. Make room for what comes next.",14,MUTED);
        LinearLayout summary=card("YOUR DAY, IN CONTEXT");
        text(summary,briefStatus,12,MUTED);
        LinearLayout periods=new LinearLayout(activity);summary.addView(periods);
        for(boolean week:new boolean[]{false,true}){Button choice=new Button(activity);choice.setText(week?"Past 7 days":"Last 24 hours");choice.setAllCaps(false);choice.setTextSize(12);choice.setTextColor(weekly==week?TEXT:MUTED);GradientDrawable pill=new GradientDrawable();pill.setColor(weekly==week?0xff344e3c:0xff1b2e25);pill.setCornerRadius(dp(18));choice.setBackground(pill);choice.setStateListAnimator(null);choice.setOnClickListener(v->{weekly=week;render();});periods.addView(choice,new LinearLayout.LayoutParams(0,dp(44),1));}
        if(!analysis.isEmpty())text(summary,analysis,16,TEXT);
        if(!analysisStatus.isEmpty())text(summary,analysisStatus,11,MUTED);
        if(notes.length()>0)action(summary,"Find my next step · on-device AI",()->{if(analyze!=null)analyze.run();});
        action(summary,loading?"Gathering notes…":"Refresh my day",()->{if(!loading){loadingBrief();refresh.run();}});
        continueCards();
        LinearLayout focus=card("YOUR FOCUS"); JSONObject selected=focused();
        text(focus, selected == null ? "One thing at a time." : selected.optString("title"),21,TEXT);
        text(focus,selected == null ? "Choose a task to keep close today." : "A personal task saved on this phone.",13,MUTED);
        action(focus,selected == null ? "Choose a focus" : "View your tasks",()->navigate.accept("Tasks"));
        learningCards();
        evidenceCards();
        if(setupView!=null){if(setupView.getParent() instanceof android.view.ViewGroup)((android.view.ViewGroup)setupView.getParent()).removeView(setupView);LinearLayout local=card("READY WHEN YOU ARE");local.addView(setupView);}
        action(body,"Ask Cully",talk);
        text(body,"Personal tasks stay on this device. Your brief loads private saved notes when you connect. Nothing is changed automatically.",11,MUTED);
    }
    private void evidenceCards(){
        if(!brief.has("last24h"))return;
        int count=0;boolean more=false;
        for(String key:weekly?new String[]{"last24h","earlier6days"}:new String[]{"last24h"}){
            JSONObject window=brief.optJSONObject(key);if(window==null)continue;
            more|=window.optBoolean("has_more");JSONArray rows=window.optJSONArray("notes");if(rows==null)continue;
            for(int i=0;i<rows.length();i++){JSONObject n=rows.optJSONObject(i);if(n==null)continue;count++;
                LinearLayout c=card(key.equals("last24h")?"LAST 24 HOURS":"EARLIER THIS WEEK");
                text(c,n.optString("summary"),16,TEXT);
                text(c,localDate(n)+" · "+n.optString("assistant"),11,MUTED);


                action(c,"Recall how / why",()->recall(n));
            }
        }
        LinearLayout coverage=card("WHAT THIS COVERS");
        text(coverage,count==0?"No saved notes in this window yet. This does not mean you did no work.":count+" sampled notes"+(more?" · more notes available":"")+". This is not a complete activity report.",13,MUTED);
        text(coverage,"Rolling "+(weekly?"7 days":"24 hours")+" ending at refresh. Times use your phone's timezone.",11,MUTED);
    }
    private void continueCards(){
        if(!brief.has("last24h"))return;
        LinearLayout intro=card("CONTINUE TODAY");
        text(intro,"Pick up a thread, on your terms.",20,TEXT);
        text(intro,"Last-planned next steps from sampled seven-day notes. They may have changed; choose what still matters.",12,MUTED);
        int count=0;
        for(int i=0;i<notes.length()&&count<3;i++){
            JSONObject note=notes.optJSONObject(i);if(note==null||value(note,"next_steps").isEmpty())continue;count++;
            text(intro,localDate(note),11,GOLD);
            text(intro,value(note,"next_steps"),16,TEXT);
            if(!value(note,"issue").isEmpty())text(intro,"Reported blocker · "+value(note,"issue"),13,MUTED);
            LinearLayout choices=new LinearLayout(activity);intro.addView(choices);
            action(choices,"Make a task",()->addTask(value(note,"next_steps")));
            action(choices,"Recall why",()->recall(note));
            for(int n=0;n<choices.getChildCount();n++)choices.getChildAt(n).setLayoutParams(new LinearLayout.LayoutParams(0,dp(48),1));
        }
        if(count==0)text(intro,"No next step was recorded in these notes. Your local focus can still guide today.",14,MUTED);
    }
    private void learningCards(){
        int count=0;
        for(int i=0;i<notes.length()&&count<2;i++){
            JSONObject note=notes.optJSONObject(i);if(note==null||value(note,"learning").isEmpty())continue;count++;
            LinearLayout learn=card("LEARN NEXT · "+localDate(note));
            text(learn,value(note,"learning"),17,TEXT);
            text(learn,"A recorded learning, not a measure of what you know. From sampled seven-day notes.",11,MUTED);
            text(learn,"Optional practice · Explain it without notes, then compare with what you saved.",13,MUTED);
            action(learn,"Draft a practice question",()->question(note,"Use saved Cully evidence to help me recall this recorded learning: "+value(note,"learning")+". Ask one question and suggest a small exercise; do not assume what I know."));
        }
    }
    private void recall(JSONObject note){
        StringBuilder detail=new StringBuilder(localDate(note));
        detail.append("\n\n").append(value(note,"summary"));
        for(String[] field:new String[][]{{"approach","How we approached it"},{"outcome","Saved outcome"},{"issue","Reported blocker"},{"learning","Recorded learning"},{"next_steps","Last planned"}})
            if(!value(note,field[0]).isEmpty())detail.append("\n\n").append(field[1]).append("\n").append(value(note,field[0]));
        detail.append("\n\nSaved preview, not live status. Missing details have not been inferred.");
        new AlertDialog.Builder(activity).setTitle(value(note,"entry_type").equals("decision")?"A saved decision":"Recall how / why").setMessage(detail)
            .setNegativeButton("Done",null).setPositiveButton("Draft a follow-up",(d,w)->question(note,"Find relevant saved Cully evidence before answering. Recall how we worked on: "+value(note,"summary")+". Explain the recorded approach and what is uncertain; do not invent a reason." )).show();
    }
    private void question(JSONObject note,String prompt){
        if(draftQuestion!=null)draftQuestion.accept(prompt);
    }
    private void tasksPage() {
        title("Make space\nfor what matters."); text(body,"Personal tasks · saved on this phone",12,MUTED);
        action(body,"+ Add a task",this::addTask);
        if(tasks.length()==0) { LinearLayout empty=card("A FRESH START"); text(empty,"What is one thing you want to move forward?",20,TEXT); text(empty,"Keep it small. You can choose a focus after adding it.",14,MUTED); }
        for(int i=0;i<tasks.length();i++) {
            JSONObject task=tasks.optJSONObject(i); if(task==null)continue;
            LinearLayout row=card(task.optBoolean("done") ? "COMPLETED" : task.optString("id").equals(focusId) ? "YOUR FOCUS" : "NEXT UP");
            text(row,task.optString("title"),19,TEXT);
            if(!task.optBoolean("done")) action(row,task.optString("id").equals(focusId)?"Clear focus":"Focus on this",()->{ focusId=task.optString("id").equals(focusId)?"":task.optString("id"); save();render(); });
            action(row,task.optBoolean("done")?"Reopen":"Mark complete",()->{ try { task.put("done",!task.optBoolean("done")); if(task.optString("id").equals(focusId))focusId="";save();render(); }catch(Exception ignored){} });
            action(row,"Edit or remove",()->editTask(task));
        }
    }
    private void advisorPage() {
        title("A useful nudge.\nRoom to decide.");
        text(body,"Based on your tasks and the saved context you loaded. Live coding-session health is not connected.",13,MUTED);
        LinearLayout next=card("A SMALL NEXT STEP");
        JSONObject selected=focused();
        text(next,selected!=null?"Start with “"+selected.optString("title")+"”.":openCount()>0?"Choose one open task to focus on.":"Capture the thing on your mind.",20,TEXT);
        text(next,"A suggestion from your local task list. You stay in control.",12,MUTED);
        action(next,openCount()>0?"Choose a task":"Add your first task",()->{ if(openCount()>0)navigate.accept("Tasks");else addTask(); });
        if(!analysis.isEmpty()){LinearLayout ai=card("YOUR LOCAL ADVISOR");text(ai,analysis,16,TEXT);text(ai,"AI suggestions from sampled saved notes. Review before acting.",11,MUTED);}
        if(notes.length()>0)action(body,"Reflect on my week · on-device AI",()->{if(analyze!=null)analyze.run();});
        boolean found=false;
        for(int i=0;i<Math.min(notes.length(),3);i++) {
            JSONObject note=notes.optJSONObject(i); if(note==null)continue;
            String issue=note.optString("issue"), nextStep=note.optString("next_steps");
            if(issue.isEmpty()&&nextStep.isEmpty())continue;
            found=true; LinearLayout card=card("FROM A SAVED HANDOFF · "+localDate(note));
            if(!issue.isEmpty())text(card,issue,16,TEXT);
            if(!nextStep.isEmpty())text(card,nextStep,15,TEXT);
            text(card,"Reported by "+note.optString("assistant")+" · may have changed since this note",11,MUTED);
            if(!nextStep.isEmpty())action(card,"Make this a personal task",()->addTask(nextStep));
        }
        if(!found){ LinearLayout empty=card("RECENT HANDOFFS"); text(empty,"Refresh your brief to see reported blockers and next steps here.",15,TEXT); }
    }
    private void addTask(){addTask("");}
    private void addTask(String seed) {
        EditText input=input(seed);
        new AlertDialog.Builder(activity).setTitle("A personal task").setMessage("Saved on this phone. It is not shared to a workspace.").setView(input)
            .setNegativeButton("Cancel",null).setPositiveButton("Add",(d,w)->{
                String title=input.getText().toString().trim(); if(title.isEmpty()||tasks.length()>=100)return;
                try{tasks.put(new JSONObject().put("id",UUID.randomUUID().toString()).put("title",title).put("done",false));save();render();}catch(Exception ignored){}
            }).show();
    }
    private void editTask(JSONObject task) {
        EditText input=input(task.optString("title"));
        new AlertDialog.Builder(activity).setTitle("Edit task").setView(input).setNegativeButton("Cancel",null)
            .setNeutralButton("Remove",(d,w)->{for(int i=0;i<tasks.length();i++)if(tasks.optJSONObject(i)==task){tasks.remove(i);break;}if(task.optString("id").equals(focusId))focusId="";save();render();})
            .setPositiveButton("Save",(d,w)->{String title=input.getText().toString().trim();if(title.isEmpty())return;try{task.put("title",title);save();render();}catch(Exception ignored){}}).show();
    }
    private EditText input(String seed){EditText input=new EditText(activity);input.setText(seed);input.setFilters(new InputFilter[]{new InputFilter.LengthFilter(240)});return input;}
    private void save(){activity.getPreferences(0).edit().putString("self_tasks",tasks.toString()).putString("self_focus",focusId).apply();}
    private JSONObject focused(){for(int i=0;i<tasks.length();i++){JSONObject task=tasks.optJSONObject(i);if(task!=null&&!task.optBoolean("done")&&task.optString("id").equals(focusId))return task;}return null;}
    private int doneCount(){int n=0;for(int i=0;i<tasks.length();i++)if(tasks.optJSONObject(i)!=null&&tasks.optJSONObject(i).optBoolean("done"))n++;return n;}
    private int openCount(){return tasks.length()-doneCount();}
    String taskContextSnapshot() {
        JSONObject data=new JSONObject();JSONArray open=new JSONArray();
        try {
            JSONObject focus=focused(); if(focus!=null)data.put("focus",focus.optString("title"));
            for(int i=0;i<tasks.length()&&open.length()<5;i++){JSONObject task=tasks.optJSONObject(i);if(task!=null&&!task.optBoolean("done")){String title=task.optString("title");open.put(title.substring(0,Math.min(title.length(),120)));}}
            data.put("open_personal_tasks_on_this_device",open).put("total_open",openCount());
        }catch(Exception ignored){}
        return data.toString();
    }
    private static String value(JSONObject n,String key){return n.isNull(key)?"":n.optString(key);}
    private static String localDate(JSONObject n){try{return java.time.OffsetDateTime.parse(n.getString("occurred_at")).atZoneSameInstant(java.time.ZoneId.systemDefault()).format(java.time.format.DateTimeFormatter.ofPattern("MMM d · h:mm a"));}catch(Exception e){return n.optString("date");}}
    private int dp(int n){return Math.round(n*getResources().getDisplayMetrics().density);}
    private LinearLayout column(){LinearLayout v=new LinearLayout(activity);v.setOrientation(1);return v;}
    private void title(String value){TextView v=text(body,value,30,TEXT);v.setTypeface(null,Typeface.BOLD);}
    private TextView text(LinearLayout parent,String value,int size,int color){TextView v=new TextView(activity);v.setText(value);v.setTextColor(color);v.setTextSize(size);v.setPadding(0,dp(5),0,dp(6));parent.addView(v);return v;}
    private LinearLayout card(String eyebrow){LinearLayout v=column();GradientDrawable bg=new GradientDrawable();bg.setColor(CARD);bg.setCornerRadius(dp(22));bg.setStroke(dp(1),0xff344a3d);v.setBackground(bg);v.setPadding(dp(18),dp(14),dp(18),dp(14));LinearLayout.LayoutParams p=new LinearLayout.LayoutParams(-1,-2);p.setMargins(0,dp(16),0,dp(4));body.addView(v,p);text(v,eyebrow,10,GOLD);return v;}
    private void action(LinearLayout parent,String label,Runnable run){Button b=new Button(activity);b.setText(label);b.setAllCaps(false);b.setGravity(Gravity.START|Gravity.CENTER_VERTICAL);b.setTextColor(GOLD);GradientDrawable surface=new GradientDrawable();surface.setColor(0xff20372b);surface.setCornerRadius(dp(18));b.setBackground(surface);b.setStateListAnimator(null);b.setMinHeight(dp(44));b.setPadding(dp(12),0,dp(12),0);b.setTextSize(13);b.setOnClickListener(v->run.run());parent.addView(b,new LinearLayout.LayoutParams(-1,-2));}
}
