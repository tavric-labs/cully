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
    private String page = "Today", focusId, briefStatus = "Connect Cully, then refresh to bring in recent work.";

    SelfHomeView(Activity activity, Runnable talk, Runnable refresh, java.util.function.Consumer<String> navigate) {
        super(activity); this.activity=activity; this.talk=talk; this.refresh=refresh; this.navigate=navigate;
        body=column(); body.setPadding(0, dp(10), 0, dp(18)); addView(body);
        try { tasks = new JSONArray(activity.getPreferences(0).getString("self_tasks", "[]")); } catch (Exception ignored) {}
        focusId=activity.getPreferences(0).getString("self_focus", "");
        render();
    }
    void showPage(String name) { page=name; render(); smoothScrollTo(0,0); }
    void loadingBrief() { briefStatus="Refreshing your recent context…"; render(); }
    void briefFailed(String message) { briefStatus=message; render(); }
    void updateBrief(String raw) {
        try {
            JSONObject result=new JSONObject(raw);
            if (result.optBoolean("isError", false)) throw new Exception();
            JSONObject data=result.optJSONObject("structuredContent");
            if (data == null) {
                JSONArray content=result.optJSONArray("content");
                if (content != null) for (int i=0; i<content.length(); i++) {
                    JSONObject part=content.getJSONObject(i);
                    if (part.optString("type").equals("text")) { data=new JSONObject(part.getString("text")); break; }
                }
            }
            if (data == null || !data.has("notes")) throw new Exception();
            notes=data.optJSONArray("notes"); if (notes == null) notes=new JSONArray();
            briefStatus="Refreshed " + new SimpleDateFormat("h:mm a",Locale.getDefault()).format(new Date()) + " · saved context, not live activity";
        } catch (Exception invalid) { briefStatus="Could not read the brief. Your local tasks are still available."; }
        render();
    }
    private void render() {
        body.removeAllViews();
        if (page.equals("Tasks")) tasksPage(); else if (page.equals("Advisor")) advisorPage(); else todayPage();
    }
    private void todayPage() {
        text(body,new SimpleDateFormat("EEEE, MMM d",Locale.getDefault()).format(new Date()).toUpperCase(Locale.getDefault()),11,GOLD);
        title("A little clarity.\nA good next step.");
        text(body,"Your work, gathered into one calm place.",14,MUTED);
        LinearLayout focus=card("YOUR FOCUS"); JSONObject selected=focused();
        text(focus, selected == null ? "One thing at a time." : selected.optString("title"),21,TEXT);
        text(focus,selected == null ? "Choose a task to keep close today." : "A personal task saved on this phone.",13,MUTED);
        action(focus,selected == null ? "Choose a focus" : "View your tasks",()->navigate.accept("Tasks"));
        LinearLayout brief=card("YOUR BRIEF");
        text(brief,openCount()+" open · "+doneCount()+" completed",18,TEXT);
        text(brief,briefStatus,12,MUTED);
        if (notes.length()==0) text(brief,"Your recent handoffs and next steps will appear here. No updates have been loaded yet.",14,TEXT);
        for (int i=0;i<Math.min(notes.length(),3);i++) {
            JSONObject note=notes.optJSONObject(i); if(note==null)continue;
            text(brief,note.optString("summary"),15,TEXT);
            text(brief,"Saved · "+note.optString("date")+" · "+note.optString("assistant"),11,MUTED);
        }
        action(brief,"Refresh brief",()->{ loadingBrief(); refresh.run(); });
        action(body,"Ask Cully",talk);
        text(body,"Personal tasks stay on this device. Your brief reads your private Cully context when you ask.",11,MUTED);
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
        boolean found=false;
        for(int i=0;i<Math.min(notes.length(),3);i++) {
            JSONObject note=notes.optJSONObject(i); if(note==null)continue;
            String issue=note.optString("issue"), nextStep=note.optString("next_steps");
            if(issue.isEmpty()&&nextStep.isEmpty())continue;
            found=true; LinearLayout card=card("FROM A SAVED HANDOFF · "+note.optString("date"));
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
    private int dp(int n){return Math.round(n*getResources().getDisplayMetrics().density);}
    private LinearLayout column(){LinearLayout v=new LinearLayout(activity);v.setOrientation(1);return v;}
    private void title(String value){TextView v=text(body,value,30,TEXT);v.setTypeface(null,Typeface.BOLD);}
    private TextView text(LinearLayout parent,String value,int size,int color){TextView v=new TextView(activity);v.setText(value);v.setTextColor(color);v.setTextSize(size);v.setPadding(0,dp(5),0,dp(6));parent.addView(v);return v;}
    private LinearLayout card(String eyebrow){LinearLayout v=column();GradientDrawable bg=new GradientDrawable();bg.setColor(CARD);bg.setCornerRadius(dp(18));v.setBackground(bg);v.setPadding(dp(18),dp(14),dp(18),dp(14));LinearLayout.LayoutParams p=new LinearLayout.LayoutParams(-1,-2);p.setMargins(0,dp(16),0,dp(4));body.addView(v,p);text(v,eyebrow,10,GOLD);return v;}
    private void action(LinearLayout parent,String label,Runnable run){Button b=new Button(activity);b.setText(label);b.setAllCaps(false);b.setGravity(Gravity.START|Gravity.CENTER_VERTICAL);b.setTextColor(GOLD);b.setBackgroundTintList(android.content.res.ColorStateList.valueOf(CARD));b.setOnClickListener(v->run.run());parent.addView(b,new LinearLayout.LayoutParams(-1,-2));}
}
