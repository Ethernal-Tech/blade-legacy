# How to Use PlantUML in Blade Project

[PlantUML][def1] is a highly versatile tool that facilitates the rapid and straightforward creation of a wide array of UML diagrams. 

In order to use it in VS Code, please install [PlantUML by jebbs][def2] (Rich PlantUML support for Visual Studio Code) extension. This extension also requires that both [Java][def3] and [Graphviz][def4] are installed.

In VS Code Preferences/Settings in the PlantUML:
- section **PlantUML: Export Out Dir** has to be set to **plantuml** and 
- section **PlantUML: Export Sub Folder** has to be unchecked.

The rest of sections have to be left as they are.
**plantuml** folder stores generated PNG files of UML diagrams created from puml files. These PNGs are later used in Gitbook documentation.

As Blade is a golang project in order to generate PlantUML files from the existing code please install [goplantuml][def5]. 
A command for PlantUML file (puml) generation is:
`goplantuml PATHtoFOLDER > FILENAME.puml` 

In the project, each top level folder holds related puml files in a uml subfolder.


[def1]: https://plantuml.com/
[def2]: https://marketplace.visualstudio.com/items?itemName=jebbs.plantuml
[def3]: http://java.com/en/download/
[def4]: http://www.graphviz.org/download/
[def5]: https://github.com/jfeliu007/goplantuml