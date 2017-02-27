/* ------------------------------------------------------------------------------
*
*  # Ace code editor
*
*  Specific JS code additions for editor_code.html page
*
*  Version: 1.0
*  Latest update: Aug 1, 2015
*
* ---------------------------------------------------------------------------- */

$(function() {


    // HTML editor
    var html_editor = ace.edit("html_editor");
        html_editor.setTheme("ace/theme/monokai");
        html_editor.getSession().setMode("ace/mode/html");
        html_editor.setShowPrintMargin(false);


    //// JSON editor
    //var json_editor = ace.edit("json_editor");
    //    json_editor.setTheme("ace/theme/monokai");
    //    json_editor.getSession().setMode("ace/mode/json");
    //    json_editor.setShowPrintMargin(false);

});
