module top;
  function int f(input int x);
    if (x > 0) return 1;
    x = 2;
    return x;
  endfunction
  task t(input int n);
    for (int i = 0; i < n; i++) begin
      if (i == 3) break;
      $display("%0d", i);
    end
    return;
    ;
  endtask
  initial begin
    fork
      disable fork;
      $display("a");
    join_none
    if (f(1) > 0) begin return_helper(); end
  end
  task return_helper(); endtask
endmodule
